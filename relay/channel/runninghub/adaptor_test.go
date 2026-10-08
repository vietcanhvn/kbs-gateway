package runninghub

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/mediaworkflow"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupImageWorkflow(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.MediaWorkflow{}))
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	mapping, _ := common.Marshal(mediaworkflow.InputMapping{Inputs: []mediaworkflow.InputBinding{
		{Role: mediaworkflow.RolePrompt, NodeID: "6", Field: "text"},
		{Role: mediaworkflow.RoleWidth, NodeID: "5", Field: "width"},
		{Role: mediaworkflow.RoleHeight, NodeID: "5", Field: "height"},
	}})
	require.NoError(t, (&model.MediaWorkflow{
		ModelName: "rh-test-image", WorkflowID: "1904136902449209346", MediaKind: mediaworkflow.KindImage,
		Executor: model.MediaWorkflowExecutorRunningHub, ApiJSON: "{}", InputMapping: string(mapping),
		OutputNodes: `["9"]`, PromptTemplate: "{}", Enabled: true,
	}).Insert())
}

func TestImageRequestRunsWorkflowAndWaitsForResult(t *testing.T) {
	setupImageWorkflow(t)
	var mu sync.Mutex
	var created map[string]any
	statusCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body := map[string]any{}
		raw, _ := io.ReadAll(r.Body)
		_ = common.Unmarshal(raw, &body)
		switch r.URL.Path {
		case "/task/openapi/create":
			created = body
			_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"taskId":"55","taskStatus":"QUEUED"}}`)
		case "/task/openapi/status":
			statusCalls++
			if statusCalls < 2 {
				_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":"RUNNING"}`)
				return
			}
			_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":"SUCCESS"}`)
		case "/task/openapi/outputs":
			_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":[{"fileUrl":"https://cdn.example/a.png","fileType":"png","nodeId":"9"},{"fileUrl":"https://cdn.example/b.png","fileType":"png","nodeId":"12"}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "rh-key", UpstreamModelName: "rh-test-image"},
	}
	adaptor := &Adaptor{httpClient: server.Client(), pollInterval: time.Millisecond}

	var request dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"rh-test-image","prompt":"a red fox","width":1024,"height":768,"response_format":"url","metadata":{"ratio":"4:3"}}`), &request))
	converted, err := adaptor.ConvertImageRequest(c, info, request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)

	resp, err := adaptor.DoRequest(c, info, strings.NewReader(string(body)))
	require.NoError(t, err)
	httpResp := resp.(*http.Response)
	require.Equal(t, http.StatusOK, httpResp.StatusCode)
	assert.Equal(t, []any{
		map[string]any{"nodeId": "6", "fieldName": "text", "fieldValue": "a red fox"},
		map[string]any{"nodeId": "5", "fieldName": "width", "fieldValue": 1024.0},
		map[string]any{"nodeId": "5", "fieldName": "height", "fieldValue": 768.0},
	}, created["nodeInfoList"])

	usage, apiErr := adaptor.DoResponse(c, httpResp, info)
	require.Nil(t, apiErr)
	assert.Equal(t, 1, usage.(*dto.Usage).TotalTokens)
	var imageResponse dto.ImageResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &imageResponse))
	require.Len(t, imageResponse.Data, 1, "only the configured output node is returned")
	assert.Equal(t, "https://cdn.example/a.png", imageResponse.Data[0].Url)
}

func TestImageRequestReturnsUpstreamFailureAsError(t *testing.T) {
	setupImageWorkflow(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/task/openapi/create":
			_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"taskId":"56"}}`)
		case "/task/openapi/status":
			_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":"FAILED"}`)
		case "/task/openapi/outputs":
			_, _ = io.WriteString(w, `{"code":805,"msg":"APIKEY_TASK_STATUS_ERROR","data":{"failedReason":{"node_id":"4","node_name":"CheckpointLoaderSimple","exception_message":"ckpt_name: 'mine.safetensors' not in []"}}}`)
		}
	}))
	defer server.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "rh-key"}}
	adaptor := &Adaptor{httpClient: server.Client(), pollInterval: time.Millisecond}
	resp, err := adaptor.DoRequest(c, info, strings.NewReader(`{"model":"rh-test-image","prompt":"x"}`))
	require.NoError(t, err)
	httpResp := resp.(*http.Response)
	assert.Equal(t, http.StatusBadGateway, httpResp.StatusCode)
	payload, _ := io.ReadAll(httpResp.Body)
	assert.Contains(t, string(payload), "missing in the RunningHub account")
}
