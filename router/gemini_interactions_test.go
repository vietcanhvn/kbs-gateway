package router

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	omniTestModel   = "gemini-omni-1.1-flash"
	omniTestGateway = "https://gw.example.test"
	omniStartQuota  = 100_000_000
)

// fakeGoogle giả lập Interactions API + Files API của Google.
type fakeGoogle struct {
	mu        sync.Mutex
	server    *httptest.Server
	keys      []string // x-goog-api-key của từng lời gọi
	bodies    []map[string]any
	nextID    int
	fileBytes []byte
	// cấu hình phản hồi POST
	status      int
	videoTokens int // 0 = không có usage
	interaction string
	// phản hồi GET /interactions/{id}
	getTokens int
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	f := &fakeGoogle{status: http.StatusOK, interaction: "completed", fileBytes: omniTestMP4(1000, 6000)}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGoogle) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, r.Header.Get("x-goog-api-key"))
	if r.URL.Query().Get("key") != "" {
		http.Error(w, "key must not be in the URL", http.StatusBadRequest)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1beta/interactions":
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = common.Unmarshal(raw, &body)
		f.bodies = append(f.bodies, body)
		if f.status != http.StatusOK {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad prompt","status":"INVALID_ARGUMENT"}}`))
			return
		}
		f.nextID++
		id := fmt.Sprintf("v1_test_%d", f.nextID)
		_, _ = w.Write([]byte(omniTestInteraction(id, f.interaction, f.videoTokens)))
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1beta/interactions/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1beta/interactions/")
		_, _ = w.Write([]byte(omniTestInteraction(id, "completed", f.getTokens)))
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, ":download"):
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(f.fileBytes)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1beta/files/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1beta/files/")
		_, _ = fmt.Fprintf(w, `{"name":"files/%s","state":"ACTIVE","uri":"https://generativelanguage.googleapis.com/v1beta/files/%s"}`, id, id)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeGoogle) lastKey() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keys[len(f.keys)-1]
}

func (f *fakeGoogle) lastBody() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[len(f.bodies)-1]
}

func omniTestInteraction(id, status string, videoTokens int) string {
	out := fmt.Sprintf(`{"id":%q,"status":%q,"model":%q,"object":"interaction"`, id, status, omniTestModel)
	if status == "completed" {
		out += fmt.Sprintf(`,"steps":[{"type":"model_output","content":[{"type":"video","mime_type":"video/mp4","uri":"https://generativelanguage.googleapis.com/v1beta/files/file_%s:download?alt=media"}]}]`, id)
	}
	if videoTokens > 0 {
		out += fmt.Sprintf(`,"usage":{"total_output_tokens":%d,"output_tokens_by_modality":[{"modality":"video","tokens":%d}]}`, videoTokens, videoTokens)
	}
	return out + "}"
}

func omniTestMP4(timescale, duration uint32) []byte {
	box := func(typ string, body []byte) []byte {
		out := make([]byte, 8, 8+len(body))
		binary.BigEndian.PutUint32(out[:4], uint32(8+len(body)))
		copy(out[4:8], typ)
		return append(out, body...)
	}
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:16], timescale)
	binary.BigEndian.PutUint32(mvhd[16:20], duration)
	return bytes.Join([][]byte{box("ftyp", []byte("isom")), box("mdat", bytes.Repeat([]byte{7}, 2048)), box("moov", box("mvhd", mvhd))}, nil)
}

type omniFixture struct {
	engine   *gin.Engine
	google   *fakeGoogle
	userID   int
	channels map[string]int // key Google → id kênh
}

func setupOmniFixture(t *testing.T) *omniFixture {
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Task{}, &model.Log{}, &model.UserSubscription{}, &model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}))

	originalMemoryCache := common.MemoryCacheEnabled
	originalServerAddress := system_setting.ServerAddress
	originalPrices := ratio_setting.ModelPrice2JSONString()
	common.MemoryCacheEnabled = false
	system_setting.ServerAddress = omniTestGateway
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"`+omniTestModel+`":0.112}`))
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCache
		system_setting.ServerAddress = originalServerAddress
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalPrices))
	})

	f := &omniFixture{google: newFakeGoogle(t), channels: map[string]int{}}
	for i, name := range []string{"user-a", "user-b"} {
		user := model.User{Username: name, Status: common.UserStatusEnabled, Group: "default", Quota: omniStartQuota, AffCode: "aff-" + name}
		require.NoError(t, model.DB.Create(&user).Error)
		require.NoError(t, model.DB.Create(&model.Token{
			UserId: user.Id, Key: fmt.Sprintf("omnitoken%d", i), Name: name, Status: common.TokenStatusEnabled,
			ExpiredTime: -1, UnlimitedQuota: true,
		}).Error)
		if i == 0 {
			f.userID = user.Id
		}
	}
	for _, key := range []string{"google-key-a", "google-key-b"} {
		baseURL := f.google.server.URL
		weight := uint(1)
		priority := int64(0)
		ch := model.Channel{
			Type: constant.ChannelTypeGemini, Key: key, Name: key, Status: common.ChannelStatusEnabled,
			BaseURL: &baseURL, Models: omniTestModel, Group: "default", Weight: &weight, Priority: &priority,
		}
		require.NoError(t, model.DB.Create(&ch).Error)
		require.NoError(t, model.DB.Create(&model.Ability{
			Group: "default", Model: omniTestModel, ChannelId: ch.Id, Enabled: true, Priority: &priority, Weight: weight,
		}).Error)
		f.channels[key] = ch.Id
	}

	f.engine = gin.New()
	SetRelayRouter(f.engine)
	return f
}

func (f *omniFixture) do(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("x-goog-api-key", token)
	f.engine.ServeHTTP(recorder, req)
	return recorder
}

func (f *omniFixture) spent(t *testing.T) int {
	t.Helper()
	quota, err := model.GetUserQuota(f.userID, true)
	require.NoError(t, err)
	return omniStartQuota - quota
}

func (f *omniFixture) task(t *testing.T, interactionID string) *model.Task {
	t.Helper()
	task, exists, err := model.GetByTaskId(f.userID, interactionID)
	require.NoError(t, err)
	require.True(t, exists)
	return task
}

func TestGeminiInteractionChargesUsageSecondsAndRewritesFiles(t *testing.T) {
	f := setupOmniFixture(t)
	f.google.videoTokens = 8 * 5792 // 8 s 720p

	resp := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0",
		`{"model":"gemini-omni-1.1-flash","input":[{"type":"text","text":"a cat surfing"}],"generation_config":{"video_config":{"task":"text_to_video"}}}`)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Thân gửi Google: giữ input, ép delivery uri; key là key kênh, không phải token cổng.
	upstream := f.google.lastBody()
	assert.Equal(t, omniTestModel, upstream["model"])
	assert.Equal(t, "uri", upstream["response_format"].(map[string]any)["delivery"])
	assert.Equal(t, "a cat surfing", upstream["input"].([]any)[0].(map[string]any)["text"])
	usedKey := f.google.lastKey()
	assert.Contains(t, f.channels, usedKey)

	// Client chỉ thấy URL của cổng.
	assert.NotContains(t, resp.Body.String(), "generativelanguage.googleapis.com")
	assert.Contains(t, resp.Body.String(), omniTestGateway+"/v1beta/files/file_v1_test_1:download?alt=media&interaction_id=v1_test_1")

	// 0,112 USD/giây × 8 s × 500000 quota/USD.
	assert.Equal(t, 448000, f.spent(t))
	task := f.task(t, "v1_test_1")
	assert.Equal(t, constant.TaskPlatformGeminiOmni, task.Platform)
	assert.Equal(t, f.channels[usedKey], task.ChannelId)
	assert.Equal(t, usedKey, task.PrivateData.Key)
	assert.Equal(t, 448000, task.Quota)
	assert.EqualValues(t, model.TaskStatusSuccess, task.Status)
	assert.Equal(t, "100%", task.Progress)

	var logRow model.Log
	require.NoError(t, model.DB.Where("user_id = ? AND type = ?", f.userID, model.LogTypeConsume).First(&logRow).Error)
	assert.Equal(t, omniTestModel, logRow.ModelName)
	assert.Equal(t, 448000, logRow.Quota)

	// Tải video qua cổng bằng đúng key đã tạo; người khác không tải được.
	download := f.do(t, http.MethodGet, "/v1beta/files/file_v1_test_1:download?alt=media&interaction_id=v1_test_1", "omnitoken0", "")
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	assert.Equal(t, f.google.fileBytes, download.Body.Bytes())
	assert.Equal(t, usedKey, f.google.lastKey())

	meta := f.do(t, http.MethodGet, "/v1beta/files/file_v1_test_1", "omnitoken0", "")
	require.Equal(t, http.StatusOK, meta.Code, meta.Body.String())
	assert.Contains(t, meta.Body.String(), `"state":"ACTIVE"`)
	assert.NotContains(t, meta.Body.String(), "generativelanguage.googleapis.com")

	other := f.do(t, http.MethodGet, "/v1beta/files/file_v1_test_1:download?alt=media", "omnitoken1", "")
	assert.Equal(t, http.StatusNotFound, other.Code)
	otherInteraction := f.do(t, http.MethodGet, "/v1beta/interactions/v1_test_1", "omnitoken1", "")
	assert.Equal(t, http.StatusNotFound, otherInteraction.Code)
	assert.Equal(t, 448000, f.spent(t), "reads never charge")
}

func TestGeminiInteractionFollowUpStaysOnOriginalChannelKey(t *testing.T) {
	f := setupOmniFixture(t)
	f.google.videoTokens = 5 * 5792

	first := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"a red car"}`)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	firstKey := f.google.lastKey()

	// Rút kênh đầu khỏi bộ chọn kênh: yêu cầu mới sẽ sang kênh kia...
	require.NoError(t, model.DB.Where("channel_id = ?", f.channels[firstKey]).Delete(&model.Ability{}).Error)
	fresh := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"a blue car"}`)
	require.Equal(t, http.StatusOK, fresh.Code, fresh.Body.String())
	assert.NotEqual(t, firstKey, f.google.lastKey())

	// ...nhưng lượt sửa tiếp theo vẫn phải về đúng kênh + key của interaction trước.
	edit := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0",
		`{"model":"gemini-omni-1.1-flash","previous_interaction_id":"v1_test_1","input":"make it night","generation_config":{"video_config":{"task":"edit"}}}`)
	require.Equal(t, http.StatusOK, edit.Code, edit.Body.String())
	assert.Equal(t, firstKey, f.google.lastKey())
	assert.Equal(t, "v1_test_1", f.google.lastBody()["previous_interaction_id"])
	assert.Equal(t, f.channels[firstKey], f.task(t, "v1_test_3").ChannelId)

	// Interaction của người khác không dùng làm previous được.
	stolen := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken1",
		`{"model":"gemini-omni-1.1-flash","previous_interaction_id":"v1_test_1","input":"x"}`)
	assert.Equal(t, http.StatusNotFound, stolen.Code)
}

func TestGeminiInteractionWithoutUsageRefundsFromDownloadedDuration(t *testing.T) {
	f := setupOmniFixture(t)
	f.google.videoTokens = 0 // Google không trả usage

	resp := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0",
		`{"model":"gemini-omni-1.1-flash","input":"rain","response_format":{"type":"video","resolution":"1080p"}}`)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	// Chưa biết thời lượng: tính tạm 10 s × 1,5 (1080p).
	assert.Equal(t, 840000, f.spent(t))
	assert.Equal(t, "99%", f.task(t, "v1_test_1").Progress)

	// Tải về lần đầu: đọc mvhd (6 s) → trả lại 4 s; lần tải sau không đổi gì.
	for i := 0; i < 2; i++ {
		download := f.do(t, http.MethodGet, "/v1beta/files/file_v1_test_1:download?alt=media", "omnitoken0", "")
		require.Equal(t, http.StatusOK, download.Code, download.Body.String())
		assert.Equal(t, 504000, f.spent(t))
	}
	task := f.task(t, "v1_test_1")
	assert.Equal(t, 504000, task.Quota)
	assert.Equal(t, "100%", task.Progress)
}

func TestGeminiInteractionUpstreamErrorIsNotCharged(t *testing.T) {
	f := setupOmniFixture(t)
	f.google.status = http.StatusBadRequest

	resp := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"x"}`)
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "INVALID_ARGUMENT")
	// Hoàn trừ trước chạy nền (BillingSession.Refund dùng gopool).
	require.Eventually(t, func() bool { return f.spent(t) == 0 }, 2*time.Second, 10*time.Millisecond)

	bad := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"x","response_format":{"resolution":"8k"}}`)
	assert.Equal(t, http.StatusBadRequest, bad.Code)
	assert.Equal(t, 0, f.spent(t))
}

func TestGeminiInteractionBackgroundSettlesWhenClientPolls(t *testing.T) {
	f := setupOmniFixture(t)
	f.google.interaction = "in_progress"
	f.google.getTokens = 4 * 5792

	resp := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0",
		`{"model":"gemini-omni-1.1-flash","input":"waves","background":true,"response_format":{"delivery":"base64"}}`)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Equal(t, "uri", f.google.lastBody()["response_format"].(map[string]any)["delivery"])
	assert.Equal(t, 560000, f.spent(t), "10 s held while running")
	assert.EqualValues(t, model.TaskStatusInProgress, f.task(t, "v1_test_1").Status)

	poll := f.do(t, http.MethodGet, "/v1beta/interactions/v1_test_1", "omnitoken0", "")
	require.Equal(t, http.StatusOK, poll.Code, poll.Body.String())
	assert.Contains(t, poll.Body.String(), omniTestGateway+"/v1beta/files/file_v1_test_1:download")
	assert.Equal(t, 224000, f.spent(t), "settled to 4 s")
	task := f.task(t, "v1_test_1")
	assert.EqualValues(t, model.TaskStatusSuccess, task.Status)
	assert.Equal(t, 224000, task.Quota)

	// Hỏi lại không tính thêm lần nào.
	again := f.do(t, http.MethodGet, "/v1beta/interactions/v1_test_1", "omnitoken0", "")
	require.Equal(t, http.StatusOK, again.Code)
	assert.Equal(t, 224000, f.spent(t))
}
