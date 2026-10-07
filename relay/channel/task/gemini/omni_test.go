package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTestMP4 dựng một tệp MP4 tối thiểu: ftyp + mdat + moov(mvhd) hoặc moov đứng trước.
func buildTestMP4(timescale, duration uint32, moovFirst bool, mdatSize int) []byte {
	box := func(typ string, body []byte) []byte {
		out := make([]byte, 8, 8+len(body))
		binary.BigEndian.PutUint32(out[:4], uint32(8+len(body)))
		copy(out[4:8], typ)
		return append(out, body...)
	}
	mvhd := make([]byte, 100) // version 0
	binary.BigEndian.PutUint32(mvhd[12:16], timescale)
	binary.BigEndian.PutUint32(mvhd[16:20], duration)
	moov := box("moov", append(box("mvhd", mvhd), box("trak", make([]byte, 40))...))
	ftyp := box("ftyp", []byte("isom\x00\x00\x02\x00isomiso2mp41"))
	mdat := box("mdat", bytes.Repeat([]byte{0xAB}, mdatSize))
	if moovFirst {
		return bytes.Join([][]byte{ftyp, moov, mdat}, nil)
	}
	return bytes.Join([][]byte{ftyp, mdat, moov}, nil)
}

func TestMP4DurationProbeReadsMvhdWhileStreaming(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		chunk     int
		want      float64
		wantFound bool
	}{
		{name: "moov at end, one byte at a time", data: buildTestMP4(1000, 8000, false, 4096), chunk: 1, want: 8, wantFound: true},
		{name: "moov first (faststart)", data: buildTestMP4(24, 144, true, 4096), chunk: 7, want: 6, wantFound: true},
		{name: "not an mp4", data: []byte("{\"error\":\"nope\"}"), chunk: 3, wantFound: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var probe MP4DurationProbe
			for i := 0; i < len(tt.data); i += tt.chunk {
				n, err := probe.Write(tt.data[i:min(i+tt.chunk, len(tt.data))])
				require.NoError(t, err)
				require.Equal(t, min(tt.chunk, len(tt.data)-i), n)
			}
			got, found := probe.Seconds()
			assert.Equal(t, tt.wantFound, found)
			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestMP4DurationProbeHandlesLargeSizeBoxAndMvhdV1(t *testing.T) {
	// mdat dùng kích thước 64-bit (size==1), mvhd version 1.
	mdatBody := bytes.Repeat([]byte{1}, 32)
	mdat := make([]byte, 16)
	binary.BigEndian.PutUint32(mdat[:4], 1)
	copy(mdat[4:8], "mdat")
	binary.BigEndian.PutUint64(mdat[8:16], uint64(16+len(mdatBody)))
	mdat = append(mdat, mdatBody...)

	mvhd := make([]byte, 112)
	mvhd[0] = 1
	binary.BigEndian.PutUint32(mvhd[20:24], 90000)
	binary.BigEndian.PutUint64(mvhd[24:32], 90000*5)
	mvhdBox := make([]byte, 8)
	binary.BigEndian.PutUint32(mvhdBox[:4], uint32(8+len(mvhd)))
	copy(mvhdBox[4:8], "mvhd")
	mvhdBox = append(mvhdBox, mvhd...)
	moov := make([]byte, 8)
	binary.BigEndian.PutUint32(moov[:4], uint32(8+len(mvhdBox)))
	copy(moov[4:8], "moov")
	moov = append(moov, mvhdBox...)

	got, found := MP4DurationSeconds(append(mdat, moov...))
	require.True(t, found)
	assert.InDelta(t, 5.0, got, 1e-9)
}

func TestParseOmniRequestValidatesBillingFields(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    *OmniRequest
		wantErr string
	}{
		{
			name: "defaults to 720p",
			body: `{"model":"gemini-omni-1.1-flash","input":"a cat"}`,
			want: &OmniRequest{Model: "gemini-omni-1.1-flash", Resolution: "720p"},
		},
		{
			name: "upper-case 4K, follow-up and task",
			body: `{"model":"gemini-omni-1.1-flash","background":true,"previous_interaction_id":"v1_abc","response_format":{"type":"video","resolution":"4K"},"generation_config":{"video_config":{"task":"extend"}}}`,
			want: &OmniRequest{Model: "gemini-omni-1.1-flash", Resolution: "4k", PreviousInteractionID: "v1_abc", Task: "extend"},
		},
		{name: "unknown resolution is a 400", body: `{"model":"m","response_format":{"resolution":"8k"}}`, wantErr: "unsupported response_format.resolution"},
		{name: "non-video output is refused", body: `{"model":"m","response_format":{"type":"text"}}`, wantErr: "must be \"video\""},
		{name: "model required", body: `{"input":"x"}`, wantErr: "model is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOmniRequest([]byte(tt.body))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildOmniUpstreamBodyIsNonBlockingAndKeepsInput(t *testing.T) {
	input := `[{"type":"text","text":"make it rain"},{"type":"image","data":"aGVsbG8=","mime_type":"image/png"}]`
	tests := []struct {
		name string
		body string
	}{
		{name: "client omitted background and delivery", body: `{"model":"gemini-omni-1.1-flash","input":` + input + `}`},
		{name: "client asked for a blocking base64 call", body: `{"model":"gemini-omni-1.1-flash","input":` + input + `,"background":false,"store":false,"response_format":{"type":"video","delivery":"base64","resolution":"1080p"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := BuildOmniUpstreamBody([]byte(tt.body), "gemini-omni-1.1-flash-upstream")
			require.NoError(t, err)
			var got map[string]any
			require.NoError(t, common.Unmarshal(out, &got))
			var wantInput any
			require.NoError(t, common.UnmarshalJsonStr(input, &wantInput))
			assert.Equal(t, wantInput, got["input"])
			assert.Equal(t, "gemini-omni-1.1-flash-upstream", got["model"])
			assert.Equal(t, true, got["background"], "create must return before the ~100 s Cloudflare Tunnel cut-off")
			assert.NotContains(t, got, "store", "background interactions must be stored")
			rf := got["response_format"].(map[string]any)
			assert.Equal(t, "uri", rf["delivery"])
			assert.Equal(t, "video", rf["type"])
		})
	}
}

func TestOmniBillingMath(t *testing.T) {
	// 5.792 token video = 1 giây 720p. Giá 0,112 USD/giây (Google 0,10 + 12%).
	const price = 0.112
	tests := []struct {
		name        string
		resolution  string
		videoTokens int
		groupRatio  float64
		wantSeconds float64
		wantQuota   int
	}{
		{name: "8s 720p", resolution: "720p", videoTokens: 8 * 5792, groupRatio: 1, wantSeconds: 8, wantQuota: 448000},
		{name: "8s 1080p costs 1.5x", resolution: "1080p", videoTokens: 8 * 5792 * 3 / 2, groupRatio: 1, wantSeconds: 8, wantQuota: 672000},
		{name: "5s 360p costs 0.3x", resolution: "360p", videoTokens: 5 * 5792 * 3 / 10, groupRatio: 1, wantSeconds: 5, wantQuota: 84000},
		{name: "10s 4k with group ratio 2", resolution: "4k", videoTokens: 10 * 5792 * 3, groupRatio: 2, wantSeconds: 10, wantQuota: 3360000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok := OmniResolutionRatio(tt.resolution)
			require.True(t, ok)
			seconds := OmniSecondsFromTokens(tt.videoTokens, ratio)
			assert.InDelta(t, tt.wantSeconds, seconds, 1e-9)
			quota, clamp := OmniQuota(price, tt.groupRatio, seconds, ratio)
			assert.Nil(t, clamp)
			assert.Equal(t, tt.wantQuota, quota)
		})
	}

	quota, _ := OmniQuota(price, 1, OmniPreChargeSeconds, 1)
	assert.Equal(t, 560000, quota, "pre-charge is 10 s")
	quota, _ = OmniQuota(price, 0, 8, 1)
	assert.Equal(t, 0, quota, "free group never charges")
	assert.Equal(t, float64(relaycommon.MaxTaskDurationSeconds), OmniSecondsFromTokens(1<<30, 1), "usage seconds are bounded")
	assert.Equal(t, OmniPreChargeSeconds, OmniFileSeconds(25), "file durations can only lower the pre-charge")
}

func TestOmniBilledSecondsPrefersUsageThenInlineMP4(t *testing.T) {
	inline := base64.StdEncoding.EncodeToString(buildTestMP4(1000, 6500, false, 128))
	tests := []struct {
		name       string
		body       string
		wantSource string
		want       float64
		wantOK     bool
	}{
		{
			name:       "usage tokens",
			body:       `{"id":"v1_a","status":"completed","usage":{"output_tokens_by_modality":[{"modality":"text","tokens":99},{"modality":"video","tokens":23168}]}}`,
			wantSource: "usage", want: 4, wantOK: true,
		},
		{
			name:       "inline base64 video",
			body:       `{"id":"v1_b","status":"completed","steps":[{"type":"model_output","content":[{"type":"video","mime_type":"video/mp4","data":"` + inline + `"}]}]}`,
			wantSource: "mp4", want: 6.5, wantOK: true,
		},
		{
			name:   "uri only, no usage",
			body:   `{"id":"v1_c","status":"completed","steps":[{"type":"model_output","content":[{"type":"video","uri":"https://generativelanguage.googleapis.com/v1beta/files/f1:download?alt=media"}]}]}`,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var interaction OmniInteraction
			require.NoError(t, common.UnmarshalJsonStr(tt.body, &interaction))
			got, source, ok := OmniBilledSeconds([]byte(tt.body), &interaction, 1)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantSource, source)
			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestRewriteOmniFileURIsHidesGoogleURLs(t *testing.T) {
	body := `{"id":"v1_x","output_video":{"uri":"https://generativelanguage.googleapis.com/v1beta/files/abc-1_Z:download?alt=media"},` +
		`"steps":[{"content":[{"type":"video","uri":"https://generativelanguage.googleapis.com/v1beta/files/abc-1_Z:download?alt=media"},{"name":"files/def2","uri":"https://generativelanguage.googleapis.com/v1beta/files/def2"}]}]}`
	out, ids := RewriteOmniFileURIs([]byte(body), "https://api.example.test/", "v1_x")

	assert.Equal(t, []string{"abc-1_Z", "def2"}, ids)
	assert.NotContains(t, string(out), "generativelanguage.googleapis.com")
	assert.Contains(t, string(out), `"https://api.example.test/v1beta/files/abc-1_Z:download?alt=media&interaction_id=v1_x"`)
	assert.Contains(t, string(out), `"https://api.example.test/v1beta/files/def2?interaction_id=v1_x"`)
	var parsed map[string]any
	require.NoError(t, common.Unmarshal(out, &parsed), "rewritten body stays valid JSON")
}

func TestOmniTaskOwnsFileMatchesWholeID(t *testing.T) {
	task := &model.Task{Platform: constant.TaskPlatformGeminiOmni}
	task.SetData(OmniTaskSummary{InteractionID: "v1_x", Files: []string{"files/abc123"}})

	assert.True(t, OmniTaskOwnsFile(task, "abc123"))
	assert.False(t, OmniTaskOwnsFile(task, "abc12"), "prefix of a stored id is not the same file")
	assert.False(t, OmniTaskOwnsFile(&model.Task{Platform: "suno", Data: task.Data}, "abc123"), "only gemini-omni tasks grant file access")
}

func TestOmniTaskAdaptorParseTaskResult(t *testing.T) {
	adaptor := &OmniTaskAdaptor{}
	tests := []struct {
		name         string
		body         string
		wantStatus   string
		wantProgress string
		wantReason   string
	}{
		{name: "completed with usage", body: `{"id":"v1","status":"completed","usage":{"output_tokens_by_modality":[{"modality":"video","tokens":5792}]}}`, wantStatus: model.TaskStatusSuccess},
		{name: "completed without usage waits for duration", body: `{"id":"v1","status":"completed"}`, wantStatus: model.TaskStatusSuccess, wantProgress: OmniProgressAwaitingDuration},
		{name: "still running", body: `{"id":"v1","status":"in_progress"}`, wantStatus: model.TaskStatusInProgress},
		{name: "failed", body: `{"id":"v1","status":"failed","error":{"message":"blocked"}}`, wantStatus: model.TaskStatusFailure, wantReason: "blocked"},
		{name: "transient upstream error keeps polling", body: `{"error":{"code":503,"message":"overloaded"}}`, wantStatus: model.TaskStatusInProgress},
		{name: "permanent upstream error fails", body: `{"error":{"code":404,"message":"not found"}}`, wantStatus: model.TaskStatusFailure, wantReason: "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := adaptor.ParseTaskResult([]byte(tt.body))
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, info.Status)
			assert.Equal(t, tt.wantProgress, info.Progress)
			assert.Equal(t, tt.wantReason, info.Reason)
		})
	}
}

func TestOmniTaskAdaptorAdjustBillingOnCompleteUsesUsage(t *testing.T) {
	task := &model.Task{
		Data: []byte(`{"id":"v1","status":"completed","usage":{"output_tokens_by_modality":[{"modality":"video","tokens":52128}]}}`),
		PrivateData: model.TaskPrivateData{BillingContext: &model.TaskBillingContext{
			ModelPrice: 0.112, GroupRatio: 1, OtherRatios: map[string]float64{"seconds": 10, "resolution": 1.5},
		}},
	}
	// 52128 token ở 1080p = 52128 / (5792 × 1.5) = 6 s → 0,112 × 500000 × 6 × 1,5
	quota := (&OmniTaskAdaptor{}).AdjustBillingOnComplete(task, nil)
	assert.Equal(t, 504000, quota)
	assert.InDelta(t, 6.0, task.PrivateData.BillingContext.OtherRatios["seconds"], 1e-9)

	task.Data = []byte(`{"id":"v1","status":"completed"}`)
	assert.Equal(t, 0, (&OmniTaskAdaptor{}).AdjustBillingOnComplete(task, nil), "no usage keeps the pre-charge")
}
