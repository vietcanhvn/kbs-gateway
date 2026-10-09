package runninghub

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/mediaworkflow"
	rh "github.com/QuantumNous/new-api/pkg/mediaworkflow/runninghub"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fakeRunningHub is a scripted RunningHub OpenAPI.
type fakeRunningHub struct {
	mu        sync.Mutex
	uploads   []string
	created   []map[string]any
	statuses  []string // returned in order by /status; the last one repeats
	outputs   string   // /outputs response once the task is finished
	cancelled bool
}

func (f *fakeRunningHub) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/task/openapi/upload" {
			assert.NoError(t, r.ParseMultipartForm(1<<20))
			assert.Equal(t, "rh-key", r.FormValue("apiKey"))
			_, header, err := r.FormFile("file")
			assert.NoError(t, err)
			name := fmt.Sprintf("api/upload-%d-%s", len(f.uploads)+1, header.Filename)
			f.uploads = append(f.uploads, name)
			_, _ = fmt.Fprintf(w, `{"code":0,"msg":"success","data":{"fileName":%q,"fileType":"input"}}`, name)
			return
		}
		body := map[string]any{}
		raw, _ := io.ReadAll(r.Body)
		assert.NoError(t, common.Unmarshal(raw, &body))
		assert.Equal(t, "rh-key", body["apiKey"])
		switch r.URL.Path {
		case "/task/openapi/create":
			f.created = append(f.created, body)
			_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"taskId":1910246754753896450,"taskStatus":"QUEUED"}}`)
		case "/task/openapi/status":
			status := f.statuses[0]
			if len(f.statuses) > 1 {
				f.statuses = f.statuses[1:]
			}
			_, _ = fmt.Fprintf(w, `{"code":0,"msg":"success","data":%q}`, status)
		case "/task/openapi/outputs":
			_, _ = io.WriteString(w, f.outputs)
		case "/task/openapi/cancel":
			f.cancelled = true
			f.statuses = []string{"FAILED"}
			_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":null}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
}

func setupRegistryDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.MediaWorkflow{}, &model.Task{}, &model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
	t.Cleanup(func() { _ = sqlDB.Close() })
}

func seedWorkflow(t *testing.T) {
	t.Helper()
	mapping := mediaworkflow.InputMapping{Inputs: []mediaworkflow.InputBinding{
		{Role: mediaworkflow.RolePrompt, NodeID: "263", Field: "text"},
		{Role: mediaworkflow.RoleImage, Index: 0, NodeID: "51", Field: "image"},
		{Role: mediaworkflow.RoleImage, Index: 1, NodeID: "199", Field: "image"},
		{Role: mediaworkflow.RoleDuration, NodeID: "259", Field: "value", ValueType: "float", Min: ptr(4), Max: ptr(15)},
		{Role: mediaworkflow.RoleAspectRatio, NodeID: "252", Field: "aspect_ratio", Enum: map[string]string{"16:9": "16:9 (Widescreen)", "9:16": "9:16 (Portrait Widescreen)"}},
		{Role: mediaworkflow.RoleSeed, NodeID: "256", Field: "noise_seed"},
	}, Billing: "per_second"}
	template := mediaworkflow.PromptTemplate{Prefix: "trigger\n", ReferenceTags: map[string]string{"image": "<Picture {n}>"}}
	mappingJSON, _ := common.Marshal(mapping)
	templateJSON, _ := common.Marshal(template)
	record := &model.MediaWorkflow{
		ModelName: "rh-test-video", WorkflowID: "2108242199912755201", MediaKind: mediaworkflow.KindVideo,
		Executor: model.MediaWorkflowExecutorRunningHub, ApiJSON: "{}", InputMapping: string(mappingJSON),
		OutputNodes: `["264"]`, PromptTemplate: string(templateJSON), Enabled: true,
	}
	require.NoError(t, record.Insert())
}

func ptr(value float64) *float64 { return &value }

func dataURL(mime, content string) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString([]byte(content))
}

func newTaskContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func TestSubmitBuildsNodeInfoListFromDCMediaRequest(t *testing.T) {
	setupRegistryDB(t)
	seedWorkflow(t)
	fake := &fakeRunningHub{}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	request := map[string]any{
		"model":    "rh-test-video",
		"prompt":   "@image1 hugs @image2",
		"duration": 20,
		"metadata": map[string]any{
			"ratio":            "9:16",
			"seed":             42,
			"reference_images": []string{dataURL("image/png", "first"), dataURL("image/jpeg", "second")},
		},
	}
	body, _ := common.Marshal(request)
	c, recorder := newTaskContext(t, string(body))
	info := &relaycommon.RelayInfo{
		OriginModelName: "rh-test-video",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public_1"},
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "rh-key", UpstreamModelName: "rh-test-video"},
	}
	adaptor := &TaskAdaptor{httpClient: server.Client()}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, map[string]float64{"seconds": 15}, adaptor.EstimateBilling(c, info), "per-second billing uses the clamped duration")

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload, _ := io.ReadAll(reader)
	var sent map[string]any
	require.NoError(t, common.Unmarshal(payload, &sent))
	assert.Equal(t, "2108242199912755201", sent["workflowId"])
	assert.Equal(t, []any{
		map[string]any{"nodeId": "263", "fieldName": "text", "fieldValue": "trigger\n<Picture 1> hugs <Picture 2>"},
		map[string]any{"nodeId": "51", "fieldName": "image", "fieldValue": "api/upload-1-ksb-input.png"},
		map[string]any{"nodeId": "199", "fieldName": "image", "fieldValue": "api/upload-2-ksb-input.jpg"},
		map[string]any{"nodeId": "259", "fieldName": "value", "fieldValue": 15.0},
		map[string]any{"nodeId": "252", "fieldName": "aspect_ratio", "fieldValue": "9:16 (Portrait Widescreen)"},
		map[string]any{"nodeId": "256", "fieldName": "noise_seed", "fieldValue": 42.0},
	}, sent["nodeInfoList"])

	resp, err := server.Client().Post(server.URL+"/task/openapi/create", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	upstreamID, taskData, taskErr := adaptor.DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "1910246754753896450|video|264", upstreamID)
	assert.JSONEq(t, `{"rh_task_id":"1910246754753896450","status":"queued","kind":"video"}`, string(taskData))
	assert.Contains(t, recorder.Body.String(), `"id":"task_public_1"`)
	assert.NotContains(t, recorder.Body.String(), "1910246754753896450", "the provider task ID is never exposed")
}

func TestSubmitRejectsMoreImagesThanTheWorkflowAccepts(t *testing.T) {
	setupRegistryDB(t)
	seedWorkflow(t)
	fake := &fakeRunningHub{}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	body, _ := common.Marshal(map[string]any{
		"model":    "rh-test-video",
		"prompt":   "three people",
		"metadata": map[string]any{"reference_images": []string{dataURL("image/png", "a"), dataURL("image/png", "b"), dataURL("image/png", "c")}},
	})
	c, _ := newTaskContext(t, string(body))
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "rh-key", UpstreamModelName: "rh-test-video"}}
	adaptor := &TaskAdaptor{httpClient: server.Client()}
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	_, err := adaptor.BuildRequestBody(c, info)
	taskErr := service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	assert.Contains(t, taskErr.Message, "at most 2 image")
	assert.Empty(t, fake.uploads, "nothing is uploaded for a request that cannot run")
}

func TestValidateRejectsUnknownWorkflowModel(t *testing.T) {
	setupRegistryDB(t)
	c, _ := newTaskContext(t, `{"model":"rh-missing","prompt":"x"}`)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "rh-missing"}}
	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "model_not_found", taskErr.Code)
}

func TestDoResponseMapsRunningHubSubmitErrors(t *testing.T) {
	tests := []struct {
		body       string
		wantStatus int
		wantText   string
	}{
		{body: `{"code":421,"msg":"TASK_QUEUE_MAXED","data":null}`, wantStatus: http.StatusTooManyRequests, wantText: "queue is full"},
		{body: `{"code":416,"msg":"TASK_CREATE_FAILED_BY_NOT_ENOUGH_WALLET","data":null}`, wantStatus: http.StatusServiceUnavailable, wantText: "balance is not enough"},
		{body: `{"code":803,"msg":"APIKEY_INVALID_NODE_INFO","data":null}`, wantStatus: http.StatusInternalServerError, wantText: "node ID / field name"},
	}
	for _, tt := range tests {
		c, _ := newTaskContext(t, "{}")
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tt.body))}
		_, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})
		require.NotNil(t, taskErr)
		assert.Equal(t, tt.wantStatus, taskErr.StatusCode)
		assert.Contains(t, taskErr.Message, tt.wantText)
	}
}

func seedPollingTask(t *testing.T, serverURL string, quota int) *model.Task {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "u", Quota: 10000, UsedQuota: quota, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 1, UserId: 1, Key: "sk-t", Name: "t", Status: common.TokenStatusEnabled, RemainQuota: 5000, UsedQuota: quota}).Error)
	baseURL := serverURL
	require.NoError(t, model.DB.Create(&model.Channel{Id: 7, Type: constant.ChannelTypeRunningHub, Name: "rh", Key: "rh-key", BaseURL: &baseURL, Status: common.ChannelStatusEnabled}).Error)
	task := &model.Task{
		TaskID: "task_poll_1", Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeRunningHub)),
		UserId: 1, ChannelId: 7, Quota: quota, Status: model.TaskStatusQueued, Group: "default",
		Data: json.RawMessage(`{}`), SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "rh-test-video"},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "1910246754753896450|video|264", BillingSource: service.BillingSourceWallet, TokenId: 1,
			BillingContext: &model.TaskBillingContext{ModelPrice: 0.1, GroupRatio: 1, OriginModelName: "rh-test-video"},
		},
	}
	require.NoError(t, model.DB.Create(task).Error)
	return task
}

func pollOnce(t *testing.T, adaptor *TaskAdaptor, task *model.Task) *model.Task {
	t.Helper()
	service.GetTaskAdaptorFunc = func(constant.TaskPlatform) service.TaskPollingAdaptor { return adaptor }
	upstreamID := task.PrivateData.UpstreamTaskID
	err := service.UpdateVideoTasks(context.Background(), task.Platform, map[int][]string{task.ChannelId: {upstreamID}}, map[string]*model.Task{upstreamID: task})
	require.NoError(t, err)
	reloaded, exists, err := model.GetByOnlyTaskId(task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	return reloaded
}

func TestPollingQueuedRunningSuccess(t *testing.T) {
	setupRegistryDB(t)
	fake := &fakeRunningHub{
		statuses: []string{"QUEUED", "RUNNING", "SUCCESS"},
		outputs: `{"code":0,"msg":"success","data":[` +
			`{"fileUrl":"https://cdn.example/preview.png","fileType":"png","nodeId":"41"},` +
			`{"fileUrl":"https://cdn.example/final.mp4","fileType":"mp4","nodeId":"264","taskCostTime":"272","consumeCoins":"120"}]}`,
	}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	task := seedPollingTask(t, server.URL, 3000)
	adaptor := &TaskAdaptor{httpClient: server.Client()}

	task = pollOnce(t, adaptor, task)
	assert.Equal(t, model.TaskStatus(model.TaskStatusQueued), task.Status)
	task = pollOnce(t, adaptor, task)
	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)
	task = pollOnce(t, adaptor, task)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assert.Equal(t, "https://cdn.example/final.mp4", task.PrivateData.UpstreamResultURL, "the configured output node wins over other outputs")
	assert.Contains(t, task.PrivateData.ResultURL, "/v1/videos/task_poll_1/content")
	assert.NotContains(t, string(task.Data), "cdn.example", "upstream URL is only kept in private data")
	assert.Equal(t, 3000, task.Quota)

	converted, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	assert.Contains(t, string(converted), "/v1/public/videos/task_poll_1/content")
	assert.NotContains(t, string(converted), "cdn.example")
}

func TestPollingFailureRefundsQuota(t *testing.T) {
	setupRegistryDB(t)
	fake := &fakeRunningHub{
		statuses: []string{"FAILED"},
		outputs:  `{"code":805,"msg":"APIKEY_TASK_STATUS_ERROR","data":{"failedReason":{"node_id":"192","node_name":"UNETLoader","exception_message":"Value not in list: unet_name: 'private.safetensors' not in []"}}}`,
	}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	task := seedPollingTask(t, server.URL, 3000)

	task = pollOnce(t, &TaskAdaptor{httpClient: server.Client()}, task)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	assert.Contains(t, task.FailReason, "missing in the RunningHub account")
	assert.Contains(t, task.FailReason, "node 192 UNETLoader")
	assert.Zero(t, task.Quota)

	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, 13000, user.Quota, "pre-consumed quota is refunded")
}

func TestCancelTaskConfirmsWithRunningHub(t *testing.T) {
	fake := &fakeRunningHub{statuses: []string{"QUEUED"}}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	adaptor := &TaskAdaptor{httpClient: server.Client()}
	require.NoError(t, adaptor.CancelTask(context.Background(), server.URL, "rh-key", "1910246754753896450|video|264", ""))
	assert.True(t, fake.cancelled)
}

func TestPickOutputs(t *testing.T) {
	outputs := []rh.Output{
		{FileURL: "a.png", FileType: "png", NodeID: "9"},
		{FileURL: "b.mp4", FileType: "mp4", NodeID: "40"},
		{FileURL: "c.mp4", FileType: "mp4", NodeID: "41"},
	}
	assert.Equal(t, "c.mp4", PickOutputs(outputs, mediaworkflow.KindVideo, []string{"41"})[0].FileURL)
	assert.Equal(t, "b.mp4", PickOutputs(outputs, mediaworkflow.KindVideo, nil)[0].FileURL)
	assert.Equal(t, "a.png", PickOutputs(outputs, mediaworkflow.KindImage, []string{"41"})[0].FileURL, "falls back to the wanted kind")
	assert.Equal(t, "a.png", PickOutputs(outputs, mediaworkflow.KindAudio, nil)[0].FileURL, "falls back to any media file")
}

func TestBilledSecondsFallsBackToWorkflowDefault(t *testing.T) {
	low, high := 3.0, 10.0
	mapping := mediaworkflow.InputMapping{Inputs: []mediaworkflow.InputBinding{
		{Role: mediaworkflow.RoleDuration, NodeID: "259", Field: "value", Min: &low, Max: &high},
	}}
	assert.Equal(t, 6, BilledSeconds(mapping, 6, 8))
	assert.Equal(t, 8, BilledSeconds(mapping, 0, 8), "no duration requested: the workflow renders its own default")
	assert.Equal(t, 10, BilledSeconds(mapping, 30, 8))
	assert.Equal(t, defaultBilledSeconds, BilledSeconds(mapping, 0, 0))

	record := &model.MediaWorkflow{
		ModelName: "rh-test", WorkflowID: "123456",
		ApiJSON:      `{"259":{"class_type":"PrimitiveFloat","inputs":{"value":8}}}`,
		InputMapping: `{"inputs":[{"role":"duration","node_id":"259","field":"value"}]}`,
	}
	workflow, err := WorkflowFromRecord(record)
	require.NoError(t, err)
	assert.Equal(t, 8.0, workflow.DefaultSeconds)
}

func TestBuildTaskSendsTheBilledDurationWhenNoneRequested(t *testing.T) {
	record := &model.MediaWorkflow{
		ModelName: "rh-test", WorkflowID: "123456",
		ApiJSON:      `{"259":{"class_type":"PrimitiveFloat","inputs":{"value":8}},"263":{"class_type":"Text","inputs":{"text":"x"}}}`,
		InputMapping: `{"inputs":[{"role":"prompt","node_id":"263","field":"text"},{"role":"duration","node_id":"259","field":"value","min":3,"max":10,"value_type":"float"}]}`,
	}
	workflow, err := WorkflowFromRecord(record)
	require.NoError(t, err)
	task, err := workflow.BuildTask(context.Background(), nil, Inputs{Prompt: "a cat"}, nil)
	require.NoError(t, err)
	var seconds any
	for _, node := range task.NodeInfoList {
		if node.NodeID == "259" {
			seconds = node.FieldValue
		}
	}
	assert.EqualValues(t, 8, seconds, "the workflow default that is billed is sent explicitly")
}
