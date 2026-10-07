package router

import (
	"bytes"
	"context"
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
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	omniTestModel   = "gemini-omni-1.1-flash"
	omniTestGateway = "https://gw.example.test"
	omniStartQuota  = 100_000_000
)

// fakeGoogle giả lập Interactions API (chỉ POST chặn) + Files API của Google.
// Giống Google thật: thân có background → 400; GET interactions → 400
// "Multiple authentication credentials" (và được đếm để test khẳng định cổng không gọi).
type fakeGoogle struct {
	mu        sync.Mutex
	server    *httptest.Server
	keys      []string // x-goog-api-key của từng lời gọi
	bodies    []map[string]any
	nextID    int
	fileBytes []byte
	// cấu hình phản hồi POST
	status          int
	videoTokens     int // 0 = không có usage
	interaction     string
	delay           time.Duration // POST chặn trả sau khoảng này
	hold            chan struct{} // khác nil: POST chờ tới khi đóng
	getInteractions int
	postsDone       int
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	f := &fakeGoogle{status: http.StatusOK, interaction: "completed", delay: 300 * time.Millisecond, fileBytes: omniTestMP4(1000, 6000)}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGoogle) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.keys = append(f.keys, r.Header.Get("x-goog-api-key"))
	f.mu.Unlock()
	if r.URL.Query().Get("key") != "" {
		http.Error(w, "key must not be in the URL", http.StatusBadRequest)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1beta/interactions":
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = common.Unmarshal(raw, &body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		delay, hold := f.delay, f.hold
		f.mu.Unlock()
		if hold != nil {
			<-hold
		}
		time.Sleep(delay)
		f.mu.Lock()
		defer f.mu.Unlock()
		defer func() { f.postsDone++ }()
		if _, ok := body["background"]; ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"test: background must not be sent","status":"INVALID_ARGUMENT"}}`))
			return
		}
		if f.status != http.StatusOK {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad prompt","status":"INVALID_ARGUMENT"}}`))
			return
		}
		f.nextID++
		id := fmt.Sprintf("v1_test_%d", f.nextID)
		_, _ = w.Write([]byte(omniTestInteraction(id, f.interaction, f.videoTokens)))
	case strings.HasPrefix(r.URL.Path, "/v1beta/interactions/"):
		f.mu.Lock()
		f.getInteractions++
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Multiple authentication credentials received. Please pass only one.","code":"invalid_request"}}`))
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, ":download"):
		if r.URL.Query().Get("alt") != "media" {
			http.Error(w, "alt=media required", http.StatusBadRequest)
			return
		}
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

func (f *fakeGoogle) interactionGets() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getInteractions
}

func (f *fakeGoogle) set(fn func(f *fakeGoogle)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
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
	// Chạy trước khi đóng Google giả và CSDL: chờ mọi lời gọi nền xong.
	t.Cleanup(func() {
		f.google.set(func(g *fakeGoogle) {
			if g.hold != nil {
				select {
				case <-g.hold:
				default:
					close(g.hold)
				}
			}
		})
		controller.WaitGeminiInteractionJobs()
	})
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
	return f.doCtx(t, context.Background(), method, path, token, body)
}

func (f *omniFixture) doCtx(t *testing.T, ctx context.Context, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
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

// create gửi POST và trả id cổng; POST phải trả ngay, trước khi Google trả lời.
func (f *omniFixture) create(t *testing.T, token, body string) string {
	t.Helper()
	started := time.Now()
	resp := f.do(t, http.MethodPost, "/v1beta/interactions", token, body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var out map[string]any
	require.NoError(t, common.Unmarshal(resp.Body.Bytes(), &out))
	assert.Equal(t, "in_progress", out["status"])
	assert.Equal(t, "interaction", out["object"])
	assert.Equal(t, omniTestModel, out["model"])
	id, _ := out["id"].(string)
	require.True(t, strings.HasPrefix(id, "gw_"), "gateway id, got %q", id)
	assert.Less(t, time.Since(started), 5*time.Second, "create must not wait for Google")
	return id
}

// waitStatus hỏi GET interaction (như KSB) tới khi ra trạng thái mong muốn.
func (f *omniFixture) waitStatus(t *testing.T, token, id, status string) map[string]any {
	t.Helper()
	var last map[string]any
	require.Eventually(t, func() bool {
		resp := f.do(t, http.MethodGet, "/v1beta/interactions/"+id, token, "")
		if resp.Code != http.StatusOK {
			return false
		}
		last = nil
		_ = common.Unmarshal(resp.Body.Bytes(), &last)
		return last["status"] == status
	}, 5*time.Second, 20*time.Millisecond, "interaction %s never became %s (last %v)", id, status, last)
	return last
}

func (f *omniFixture) refundLogs(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, model.DB.Model(&model.Log{}).Where("user_id = ? AND type = ?", f.userID, model.LogTypeRefund).Count(&n).Error)
	return n
}

// Luồng KSB: POST (trả ngay in_progress, id cổng) → hỏi GET interaction tới
// completed → GET metadata tệp tới ACTIVE → GET :download. Client ngắt ngay
// sau POST cũng không làm mất kết quả.
func TestGeminiInteractionKSBFlowChargesOnceFromUsage(t *testing.T) {
	f := setupOmniFixture(t)
	f.google.set(func(g *fakeGoogle) { g.videoTokens = 8 * 5792 }) // 8 s 720p

	reqCtx, disconnect := context.WithCancel(context.Background())
	started := time.Now()
	createResp := f.doCtx(t, reqCtx, http.MethodPost, "/v1beta/interactions", "omnitoken0",
		`{"model":"gemini-omni-1.1-flash","input":[{"type":"text","text":"a cat surfing"}],"background":true,"response_format":{"type":"video","delivery":"uri","aspect_ratio":"9:16"},"generation_config":{"video_config":{"task":"text_to_video"}}}`)
	disconnect() // client đi mất ngay sau khi nhận id
	require.Equal(t, http.StatusOK, createResp.Code, createResp.Body.String())
	assert.Less(t, time.Since(started), 300*time.Millisecond, "create returns before Google answers")
	var created map[string]any
	require.NoError(t, common.Unmarshal(createResp.Body.Bytes(), &created))
	id := created["id"].(string)
	require.True(t, strings.HasPrefix(id, "gw_"))
	assert.Equal(t, "in_progress", created["status"])

	// Đang chạy: giữ trừ trước 10 s; GET trả in_progress (không gọi Google).
	assert.Equal(t, 560000, f.spent(t))
	task := f.task(t, id)
	assert.Equal(t, constant.TaskPlatformGeminiOmni, task.Platform)
	assert.EqualValues(t, model.TaskStatusInProgress, task.Status)
	running := f.do(t, http.MethodGet, "/v1beta/interactions/"+id, "omnitoken0", "")
	require.Equal(t, http.StatusOK, running.Code)
	assert.Contains(t, running.Body.String(), `"status":"in_progress"`)

	done := f.waitStatus(t, "omnitoken0", id, "completed")
	controller.WaitGeminiInteractionJobs() // quyết toán chạy ngay sau khi ghi completed
	assert.Equal(t, id, done["id"], "Google id is replaced by the gateway id")

	// Thân gửi Google: POST chặn, không background, delivery uri; key là key kênh.
	upstream := f.google.lastBody()
	assert.Equal(t, omniTestModel, upstream["model"])
	assert.NotContains(t, upstream, "background")
	assert.Equal(t, "uri", upstream["response_format"].(map[string]any)["delivery"])
	assert.Equal(t, "9:16", upstream["response_format"].(map[string]any)["aspect_ratio"])
	assert.Equal(t, "a cat surfing", upstream["input"].([]any)[0].(map[string]any)["text"])
	usedKey := f.google.lastKey()
	assert.Contains(t, f.channels, usedKey)

	// Hỏi nhiều lần: quyết toán đúng một lần, về 8 s (0,112 × 8 × 500000).
	for i := 0; i < 3; i++ {
		poll := f.do(t, http.MethodGet, "/v1beta/interactions/"+id, "omnitoken0", "")
		require.Equal(t, http.StatusOK, poll.Code, poll.Body.String())
		assert.Contains(t, poll.Body.String(), `"status":"completed"`)
		assert.NotContains(t, poll.Body.String(), "generativelanguage.googleapis.com")
		assert.NotContains(t, poll.Body.String(), `"v1_test_1"`, "Google interaction id stays internal")
		assert.Contains(t, poll.Body.String(), omniTestGateway+"/v1beta/files/file_v1_test_1:download?alt=media&interaction_id="+id)
		assert.Equal(t, 448000, f.spent(t))
	}
	task = f.task(t, id)
	assert.EqualValues(t, model.TaskStatusSuccess, task.Status)
	assert.Equal(t, "100%", task.Progress)
	assert.Equal(t, 448000, task.Quota)
	assert.Equal(t, "v1_test_1", task.PrivateData.UpstreamTaskID)
	assert.Equal(t, f.channels[usedKey], task.ChannelId)
	assert.Equal(t, usedKey, task.PrivateData.Key)

	var consumeLog model.Log
	require.NoError(t, model.DB.Where("user_id = ? AND type = ?", f.userID, model.LogTypeConsume).First(&consumeLog).Error)
	assert.Equal(t, omniTestModel, consumeLog.ModelName)

	// Tệp: metadata rồi tải, đúng như KSB gọi (kèm interaction_id = id cổng) và không kèm.
	for _, suffix := range []string{"?interaction_id=" + id, ""} {
		meta := f.do(t, http.MethodGet, "/v1beta/files/file_v1_test_1"+suffix, "omnitoken0", "")
		require.Equal(t, http.StatusOK, meta.Code, meta.Body.String())
		assert.Contains(t, meta.Body.String(), `"state":"ACTIVE"`)
		assert.NotContains(t, meta.Body.String(), "generativelanguage.googleapis.com")
		assert.Equal(t, usedKey, f.google.lastKey())
	}
	download := f.do(t, http.MethodGet, "/v1beta/files/file_v1_test_1:download?alt=media&interaction_id="+id, "omnitoken0", "")
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	assert.Equal(t, f.google.fileBytes, download.Body.Bytes())
	assert.Equal(t, usedKey, f.google.lastKey())
	download = f.do(t, http.MethodGet, "/v1beta/files/file_v1_test_1:download", "omnitoken0", "")
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	assert.Equal(t, 448000, f.spent(t), "reads never charge")

	// Người dùng khác không đọc được interaction / tệp.
	assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodGet, "/v1beta/files/file_v1_test_1:download?interaction_id="+id, "omnitoken1", "").Code)
	assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodGet, "/v1beta/files/file_v1_test_1:download", "omnitoken1", "").Code)
	assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodGet, "/v1beta/interactions/"+id, "omnitoken1", "").Code)
	assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodPost, "/v1beta/interactions/"+id+":cancel", "omnitoken1", "").Code)

	assert.Equal(t, 0, f.google.interactionGets(), "the gateway never calls Google GET interactions")
}

func TestGeminiInteractionFollowUpTranslatesGatewayIDAndKeepsChannel(t *testing.T) {
	f := setupOmniFixture(t)

	first := f.create(t, "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"a red car"}`)
	f.waitStatus(t, "omnitoken0", first, "completed")
	firstKey := f.google.lastKey()

	// Rút kênh đầu khỏi bộ chọn kênh: yêu cầu mới sẽ sang kênh kia...
	require.NoError(t, model.DB.Where("channel_id = ?", f.channels[firstKey]).Delete(&model.Ability{}).Error)
	fresh := f.create(t, "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"a blue car"}`)
	f.waitStatus(t, "omnitoken0", fresh, "completed")
	assert.NotEqual(t, firstKey, f.google.lastKey())

	// ...nhưng lượt sửa tiếp theo (gửi id cổng) về đúng kênh + key cũ, với id Google.
	edit := f.create(t, "omnitoken0",
		`{"model":"gemini-omni-1.1-flash","background":true,"previous_interaction_id":"`+first+`","input":"make it night","generation_config":{"video_config":{"task":"edit"}}}`)
	f.waitStatus(t, "omnitoken0", edit, "completed")
	assert.Equal(t, firstKey, f.google.lastKey())
	assert.Equal(t, "v1_test_1", f.google.lastBody()["previous_interaction_id"])
	assert.Equal(t, f.channels[firstKey], f.task(t, edit).ChannelId)
	assert.Equal(t, "v1_test_3", f.task(t, edit).PrivateData.UpstreamTaskID)

	// Interaction của người khác không dùng làm previous được.
	stolen := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken1",
		`{"model":"gemini-omni-1.1-flash","previous_interaction_id":"`+first+`","input":"x"}`)
	assert.Equal(t, http.StatusNotFound, stolen.Code)

	// Interaction trước còn đang chạy (chưa có id Google) → 400, không trừ tiền.
	f.google.set(func(g *fakeGoogle) { g.hold = make(chan struct{}) })
	pending := f.create(t, "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"slow"}`)
	spent := f.spent(t)
	early := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0",
		`{"model":"gemini-omni-1.1-flash","previous_interaction_id":"`+pending+`","input":"x"}`)
	assert.Equal(t, http.StatusBadRequest, early.Code, early.Body.String())
	assert.Equal(t, spent, f.spent(t))
	assert.Equal(t, 0, f.google.interactionGets())
}

func TestGeminiInteractionWithoutUsageRefundsFromDownloadedDuration(t *testing.T) {
	f := setupOmniFixture(t) // Google không trả usage

	id := f.create(t, "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"rain","response_format":{"type":"video","resolution":"1080p"}}`)
	f.waitStatus(t, "omnitoken0", id, "completed")
	controller.WaitGeminiInteractionJobs()
	// Chưa biết thời lượng: giữ tạm 10 s × 1,5 (1080p).
	assert.Equal(t, 840000, f.spent(t))
	assert.Equal(t, "99%", f.task(t, id).Progress)

	// Tải về lần đầu: đọc mvhd (6 s) → trả lại 4 s; lần tải sau không đổi gì.
	for i := 0; i < 2; i++ {
		download := f.do(t, http.MethodGet, "/v1beta/files/file_v1_test_1:download?alt=media&interaction_id="+id, "omnitoken0", "")
		require.Equal(t, http.StatusOK, download.Code, download.Body.String())
		assert.Equal(t, 504000, f.spent(t))
	}
	task := f.task(t, id)
	assert.Equal(t, 504000, task.Quota)
	assert.Equal(t, "100%", task.Progress)
}

func TestGeminiInteractionUpstreamErrorFailsAndRefunds(t *testing.T) {
	f := setupOmniFixture(t)
	f.google.set(func(g *fakeGoogle) { g.status = http.StatusBadRequest })

	id := f.create(t, "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"x"}`)
	assert.Equal(t, 560000, f.spent(t), "pre-charged while running")
	failed := f.waitStatus(t, "omnitoken0", id, "failed")
	assert.Equal(t, "bad prompt", failed["error"].(map[string]any)["message"])
	controller.WaitGeminiInteractionJobs() // hoàn tiền chạy ngay sau khi ghi failed
	assert.Equal(t, 0, f.spent(t))
	task := f.task(t, id)
	assert.EqualValues(t, model.TaskStatusFailure, task.Status)
	assert.Equal(t, 0, task.Quota)
	assert.EqualValues(t, 1, f.refundLogs(t))

	// Không có id Google → không dùng làm previous được.
	follow := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0", `{"model":"gemini-omni-1.1-flash","previous_interaction_id":"`+id+`","input":"x"}`)
	assert.Equal(t, http.StatusBadRequest, follow.Code)

	// Lỗi kiểm tra đầu vào: 400 ngay, không trừ tiền, không gọi Google.
	bad := f.do(t, http.MethodPost, "/v1beta/interactions", "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"x","response_format":{"resolution":"8k"}}`)
	assert.Equal(t, http.StatusBadRequest, bad.Code)
	assert.Equal(t, 0, f.spent(t))
}

func TestGeminiInteractionCancelRefundsOnceAndIgnoresLateResult(t *testing.T) {
	f := setupOmniFixture(t)
	hold := make(chan struct{})
	f.google.set(func(g *fakeGoogle) { g.hold = hold; g.videoTokens = 5 * 5792 })

	id := f.create(t, "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"waves"}`)
	assert.Equal(t, 560000, f.spent(t))

	for i := 0; i < 2; i++ {
		cancel := f.do(t, http.MethodPost, "/v1beta/interactions/"+id+":cancel", "omnitoken0", "")
		require.Equal(t, http.StatusOK, cancel.Code, cancel.Body.String())
		assert.Contains(t, cancel.Body.String(), `"status":"cancelled"`)
		assert.Contains(t, cancel.Body.String(), `"id":"`+id+`"`)
		assert.Equal(t, 0, f.spent(t))
	}

	// Google trả kết quả muộn: bỏ qua, vẫn huỷ, vẫn hoàn tiền.
	f.google.set(func(g *fakeGoogle) { close(g.hold); g.hold = nil })
	controller.WaitGeminiInteractionJobs()
	f.waitStatus(t, "omnitoken0", id, "cancelled")
	task := f.task(t, id)
	assert.EqualValues(t, model.TaskStatusFailure, task.Status)
	assert.Equal(t, 0, task.Quota)
	assert.Empty(t, task.PrivateData.UpstreamTaskID)
	assert.Equal(t, 0, f.spent(t))
	assert.EqualValues(t, 1, f.refundLogs(t))
	assert.Equal(t, 0, f.google.interactionGets())
}

func omniWirePoller(t *testing.T) {
	previousFactory := service.GetTaskAdaptorFunc
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor {
		if a := relay.GetTaskAdaptor(platform); a != nil {
			return a
		}
		return nil
	}
	previousLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 100
	t.Cleanup(func() {
		service.GetTaskAdaptorFunc = previousFactory
		constant.TaskQueryLimit = previousLimit
	})
}

// Cổng khởi động lại giữa lời gọi Google: bộ poll (không hỏi Google) đánh thất
// bại + hoàn tiền sau GeminiOmniStaleAfter; kết quả về muộn không ghi đè. Dòng
// cũ (id Google, tạo bằng background) cũng được dọn mà không gọi Google.
func TestGeminiInteractionPollerFinalizesLostCallsWithoutCallingGoogle(t *testing.T) {
	f := setupOmniFixture(t)
	omniWirePoller(t)
	hold := make(chan struct{})
	f.google.set(func(g *fakeGoogle) { g.hold = hold; g.videoTokens = 3 * 5792 })

	stale := f.create(t, "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"snow"}`)
	recent := f.create(t, "omnitoken0", `{"model":"gemini-omni-1.1-flash","input":"sun"}`)
	assert.Equal(t, 2*560000, f.spent(t))

	// Dòng cũ: task_id = id Google, đang chạy, đã trừ 10 s.
	legacy := &model.Task{
		TaskID: "v1_legacy", Platform: constant.TaskPlatformGeminiOmni, UserId: f.userID, Group: "default",
		ChannelId: f.channels["google-key-a"], Status: model.TaskStatusInProgress, Progress: "30%", Quota: 560000,
		SubmitTime:  time.Now().Add(-2 * time.Hour).Unix(),
		PrivateData: model.TaskPrivateData{Key: "google-key-a", UpstreamTaskID: "v1_legacy"},
	}
	require.NoError(t, legacy.Insert())
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", f.userID).Update("quota", gorm.Expr("quota - ?", 560000)).Error)
	assert.Equal(t, 3*560000, f.spent(t))

	old := time.Now().Add(-constant.GeminiOmniStaleAfter - time.Minute).Unix()
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id = ?", stale).Update("submit_time", old).Error)

	service.RunTaskPollingOnce(context.Background(), nil)

	failed := f.waitStatus(t, "omnitoken0", stale, "failed")
	assert.Contains(t, failed["error"].(map[string]any)["message"], "upstream result was lost")
	f.waitStatus(t, "omnitoken0", "v1_legacy", "failed")
	assert.EqualValues(t, model.TaskStatusInProgress, f.task(t, recent).Status, "recent calls are left alone")
	assert.Equal(t, 560000, f.spent(t), "stale and legacy rows refunded")

	// Google trả lời cả hai lời gọi: lời gọi mới quyết toán 3 s, lời gọi đã dọn bị bỏ qua.
	f.google.set(func(g *fakeGoogle) { close(g.hold); g.hold = nil })
	controller.WaitGeminiInteractionJobs()
	f.waitStatus(t, "omnitoken0", recent, "completed")
	f.waitStatus(t, "omnitoken0", stale, "failed")
	assert.Equal(t, 168000, f.spent(t))
	// Hoàn: lời gọi treo, dòng cũ, và phần thừa (10 s → 3 s) của lời gọi mới.
	assert.EqualValues(t, 3, f.refundLogs(t))

	service.RunTaskPollingOnce(context.Background(), nil)
	assert.Equal(t, 168000, f.spent(t), "a second pass changes nothing")
	assert.Equal(t, 0, f.google.interactionGets(), "the poller never calls Google GET interactions")
}

// Dòng cũ đã hoàn tất (phản hồi Google gốc lưu trong data) vẫn đọc được bằng id Google.
func TestGeminiInteractionLegacyCompletedRowIsServedFromDB(t *testing.T) {
	f := setupOmniFixture(t)
	legacy := &model.Task{
		TaskID: "v1_done", Platform: constant.TaskPlatformGeminiOmni, UserId: f.userID, Group: "default",
		ChannelId: f.channels["google-key-b"], Status: model.TaskStatusSuccess, Progress: "100%",
		SubmitTime: time.Now().Unix(), Data: []byte(omniTestInteraction("v1_done", "completed", 5792)),
		PrivateData: model.TaskPrivateData{Key: "google-key-b", UpstreamTaskID: "v1_done"},
	}
	require.NoError(t, legacy.Insert())

	done := f.waitStatus(t, "omnitoken0", "v1_done", "completed")
	assert.Equal(t, "v1_done", done["id"])
	resp := f.do(t, http.MethodGet, "/v1beta/interactions/v1_done", "omnitoken0", "")
	assert.Contains(t, resp.Body.String(), omniTestGateway+"/v1beta/files/file_v1_done:download?alt=media&interaction_id=v1_done")
	download := f.do(t, http.MethodGet, "/v1beta/files/file_v1_done:download?alt=media&interaction_id=v1_done", "omnitoken0", "")
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	assert.Equal(t, "google-key-b", f.google.lastKey())

	// Lượt sửa tiếp theo từ dòng cũ dùng chính id Google đó.
	edit := f.create(t, "omnitoken0", `{"model":"gemini-omni-1.1-flash","previous_interaction_id":"v1_done","input":"again"}`)
	f.waitStatus(t, "omnitoken0", edit, "completed")
	assert.Equal(t, "v1_done", f.google.lastBody()["previous_interaction_id"])
	assert.Equal(t, "google-key-b", f.google.lastKey())
	assert.Equal(t, 0, f.google.interactionGets())
}
