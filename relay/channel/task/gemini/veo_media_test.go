package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	veoTestFirst = "data:image/png;base64,Rk1SU1Q="
	veoTestLast  = "data:image/jpeg;base64,TEFTVA=="
	veoTestRef1  = "data:image/png;base64,UkVGMQ=="
	veoTestRef2  = "data:image/webp;base64,UkVGMg=="
)

func newVeoTestContext(t *testing.T, body, model string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = request
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: model},
	}
	return c, info
}

// runVeo drives the adaptor like relay_task.go: validate -> estimate billing -> build body.
func runVeo(t *testing.T, body, model string) (map[string]any, map[string]float64, *relaycommon.RelayInfo) {
	t.Helper()
	c, info := newVeoTestContext(t, body, model)
	a := &TaskAdaptor{}
	taskErr := a.ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)
	ratios := a.EstimateBilling(c, info)
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	raw, err := io.ReadAll(reader)
	require.NoError(t, err)
	t.Logf("upstream body: %s", raw)
	var out map[string]any
	require.NoError(t, common.Unmarshal(raw, &out))
	return out, ratios, info
}

func veoInstance(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	instances, ok := body["instances"].([]any)
	require.True(t, ok)
	require.Len(t, instances, 1)
	return instances[0].(map[string]any)
}

func TestVeoFirstFrameOnly(t *testing.T) {
	body, ratios, info := runVeo(t, `{
		"model":"veo-3.1-generate-preview","prompt":"a cat walks","duration":6,
		"width":1280,"height":720,"size":"1280x720",
		"image":"`+veoTestFirst+`",
		"metadata":{"ratio":"16:9","resolution":"720p"}
	}`, "veo-3.1-generate-preview")

	inst := veoInstance(t, body)
	assert.Equal(t, "a cat walks", inst["prompt"])
	assert.Equal(t, map[string]any{"bytesBase64Encoded": "Rk1SU1Q=", "mimeType": "image/png"}, inst["image"])
	assert.NotContains(t, inst, "lastFrame")
	assert.NotContains(t, inst, "referenceImages")
	params := body["parameters"].(map[string]any)
	assert.EqualValues(t, 6, params["durationSeconds"])
	assert.Equal(t, "16:9", params["aspectRatio"])
	assert.Equal(t, "720p", params["resolution"])
	assert.EqualValues(t, 6, ratios["seconds"])
	assert.Equal(t, constant.TaskActionGenerate, info.Action)
}

func TestVeoFirstAndLastFrameForcesEightSeconds(t *testing.T) {
	body, ratios, _ := runVeo(t, `{
		"model":"veo-3.1-generate-preview","prompt":"morph","duration":6,
		"width":720,"height":1280,"size":"720x1280",
		"image":"`+veoTestFirst+`",
		"metadata":{"last_frame_image":"`+veoTestLast+`"}
	}`, "veo-3.1-generate-preview")

	inst := veoInstance(t, body)
	assert.Equal(t, map[string]any{"bytesBase64Encoded": "Rk1SU1Q=", "mimeType": "image/png"}, inst["image"])
	assert.Equal(t, map[string]any{"bytesBase64Encoded": "TEFTVA==", "mimeType": "image/jpeg"}, inst["lastFrame"])
	params := body["parameters"].(map[string]any)
	assert.EqualValues(t, 8, params["durationSeconds"])
	assert.Equal(t, "9:16", params["aspectRatio"])
	assert.EqualValues(t, 8, ratios["seconds"], "billing must use the duration actually sent")
}

func TestVeoThreeReferenceImagesForcesEightSeconds(t *testing.T) {
	// Third reference is raw base64 (no data: prefix) - also accepted.
	body, ratios, info := runVeo(t, `{
		"model":"veo-3.1-generate-preview","prompt":"the hero in a market","duration":4,
		"width":1280,"height":720,"size":"1280x720",
		"metadata":{"reference_images":["`+veoTestRef1+`","`+veoTestRef2+`","iVBORw0KGgo="],"durationSeconds":4}
	}`, "veo-3.1-generate-preview")

	inst := veoInstance(t, body)
	assert.NotContains(t, inst, "image")
	assert.NotContains(t, inst, "lastFrame")
	assert.Equal(t, []any{
		map[string]any{"image": map[string]any{"bytesBase64Encoded": "UkVGMQ==", "mimeType": "image/png"}, "referenceType": "ASSET"},
		map[string]any{"image": map[string]any{"bytesBase64Encoded": "UkVGMg==", "mimeType": "image/webp"}, "referenceType": "ASSET"},
		map[string]any{"image": map[string]any{"bytesBase64Encoded": "iVBORw0KGgo=", "mimeType": "image/png"}, "referenceType": "ASSET"},
	}, inst["referenceImages"])
	params := body["parameters"].(map[string]any)
	assert.EqualValues(t, 8, params["durationSeconds"])
	assert.EqualValues(t, 8, ratios["seconds"])
	assert.Equal(t, constant.TaskActionGenerate, info.Action)
}

func TestVeoReferenceImageFromURLIsDownloaded(t *testing.T) {
	orig := fetchVeoImageURL
	defer func() { fetchVeoImageURL = orig }()
	var fetched []string
	fetchVeoImageURL = func(url string) (string, string, error) {
		fetched = append(fetched, url)
		return "image/jpeg; charset=binary", "SlBFRw==", nil
	}
	body, _, _ := runVeo(t, `{
		"model":"veo-3.1-generate-preview","prompt":"p","duration":8,
		"metadata":{"reference_images":["https://example.com/a.jpg"]}
	}`, "veo-3.1-generate-preview")
	assert.Equal(t, []string{"https://example.com/a.jpg"}, fetched)
	assert.Equal(t, []any{
		map[string]any{"image": map[string]any{"bytesBase64Encoded": "SlBFRw==", "mimeType": "image/jpeg"}, "referenceType": "ASSET"},
	}, veoInstance(t, body)["referenceImages"])
}

func TestVeoMediaErrors(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		model    string
		wantCode string
		wantMsg  string
	}{
		{
			name:     "four reference images",
			body:     `{"model":"m","prompt":"p","metadata":{"reference_images":["` + veoTestRef1 + `","` + veoTestRef1 + `","` + veoTestRef1 + `","` + veoTestRef1 + `"]}}`,
			model:    "veo-3.1-generate-preview",
			wantCode: "too_many_reference_images",
			wantMsg:  "veo accepts at most 3 reference images, got 4",
		},
		{
			name:     "reference images with first frame",
			body:     `{"model":"m","prompt":"p","image":"` + veoTestFirst + `","metadata":{"reference_images":["` + veoTestRef1 + `"]}}`,
			model:    "veo-3.1-generate-preview",
			wantCode: "conflicting_media_inputs",
			wantMsg:  "top-level image cannot be combined with metadata.reference_images",
		},
		{
			name:     "last frame without first frame",
			body:     `{"model":"m","prompt":"p","metadata":{"last_frame_image":"` + veoTestLast + `"}}`,
			model:    "veo-3.1-generate-preview",
			wantCode: "missing_first_frame",
			wantMsg:  "veo last_frame_image requires image (first frame)",
		},
		{
			name:     "reference videos",
			body:     `{"model":"m","prompt":"p","metadata":{"reference_videos":["https://example.com/a.mp4"]}}`,
			model:    "veo-3.1-generate-preview",
			wantCode: "unsupported_media_field",
			wantMsg:  "veo does not support metadata.reference_videos or metadata.reference_audios",
		},
		{
			name:     "reference images on veo 3.0",
			body:     `{"model":"m","prompt":"p","metadata":{"reference_images":["` + veoTestRef1 + `"]}}`,
			model:    "veo-3.0-generate-001",
			wantCode: "unsupported_model_feature",
			wantMsg:  "model veo-3.0-generate-001 does not support reference_images (Veo 3.1 / 3.1 Fast only)",
		},
		{
			name:     "invalid reference image",
			body:     `{"model":"m","prompt":"p","metadata":{"reference_images":["not base64!"]}}`,
			model:    "veo-3.1-generate-preview",
			wantCode: "invalid_media",
			wantMsg:  "metadata.reference_images[0] must be a data URL, base64 image or http(s) URL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, info := newVeoTestContext(t, tt.body, tt.model)
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, tt.wantCode, taskErr.Code)
			assert.Equal(t, tt.wantMsg, taskErr.Message)
			assert.True(t, taskErr.LocalError)
		})
	}
}

// The DC-Media validator already rejects image + reference_images; the Veo
// resolver keeps its own guard for callers that bypass it.
func TestResolveVeoMediaRejectsReferencesWithFrames(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-generate-preview"}}
	for _, req := range []relaycommon.TaskSubmitReq{
		{Prompt: "p", Image: veoTestFirst, Metadata: map[string]any{"reference_images": []any{veoTestRef1}}},
		{Prompt: "p", Images: []string{veoTestFirst}, Metadata: map[string]any{"reference_images": []any{veoTestRef1}, "last_frame_image": veoTestLast}},
	} {
		_, err := ResolveVeoMedia(nil, info, req)
		require.Error(t, err)
		assert.Equal(t, "veo reference_images cannot be combined with image (first frame) or last_frame_image", err.Error())
	}
}
