package runninghub

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T, handler func(path string, body map[string]any, r *http.Request) string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer key-123", r.Header.Get("Authorization"))
		body := map[string]any{}
		if r.Header.Get("Content-Type") == "application/json" {
			raw, _ := io.ReadAll(r.Body)
			require.NoError(t, common.Unmarshal(raw, &body))
		}
		_, _ = io.WriteString(w, handler(r.URL.Path, body, r))
	}))
	t.Cleanup(server.Close)
	return New(server.URL, "key-123", server.Client())
}

func TestClientUploadSendsMultipartAndReturnsFileName(t *testing.T) {
	client := newTestServer(t, func(path string, _ map[string]any, r *http.Request) string {
		require.Equal(t, "/task/openapi/upload", path)
		require.NoError(t, r.ParseMultipartForm(1<<20))
		assert.Equal(t, "key-123", r.FormValue("apiKey"))
		assert.Equal(t, "input", r.FormValue("fileType"))
		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		data, _ := io.ReadAll(file)
		assert.Equal(t, "ref.png", header.Filename)
		assert.Equal(t, "png-bytes", string(data))
		return `{"code":0,"msg":"success","data":{"fileName":"api/abc.png","fileType":"input"}}`
	})
	name, err := client.Upload(context.Background(), "ref.png", []byte("png-bytes"))
	require.NoError(t, err)
	assert.Equal(t, "api/abc.png", name)
}

func TestClientCreateTaskKeepsSnowflakeIDExact(t *testing.T) {
	client := newTestServer(t, func(path string, body map[string]any, _ *http.Request) string {
		require.Equal(t, "/task/openapi/create", path)
		assert.Equal(t, "key-123", body["apiKey"])
		assert.Equal(t, "2108242199912755201", body["workflowId"])
		assert.Equal(t, "plus", body["instanceType"])
		assert.Equal(t, []any{map[string]any{"nodeId": "263", "fieldName": "text", "fieldValue": "hello"}}, body["nodeInfoList"])
		return `{"code":0,"msg":"success","data":{"taskId":1910246754753896450,"taskStatus":"QUEUED","promptTips":"{}"}}`
	})
	created, err := client.CreateTask(context.Background(), CreateTaskRequest{
		WorkflowID:   "2108242199912755201",
		InstanceType: "plus",
		NodeInfoList: []NodeInfo{{NodeID: "263", FieldName: "text", FieldValue: "hello"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "1910246754753896450", created.TaskID)
	assert.Equal(t, "QUEUED", created.TaskStatus)
}

func TestClientCreateTaskExplainsErrorCodes(t *testing.T) {
	client := newTestServer(t, func(string, map[string]any, *http.Request) string {
		return `{"code":416,"msg":"TASK_CREATE_FAILED_BY_NOT_ENOUGH_WALLET","data":null}`
	})
	_, err := client.CreateTask(context.Background(), CreateTaskRequest{WorkflowID: "1"})
	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, CodeNotEnoughWallet, apiErr.Code)
	assert.Contains(t, err.Error(), "balance is not enough")
	assert.False(t, apiErr.Retryable())
	assert.True(t, (&APIError{Code: CodeQueueMaxed}).Retryable())
}

func TestClientTaskOutputsStates(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     OutputsResult
	}{
		{name: "queued", response: `{"code":813,"msg":"APIKEY_TASK_IS_QUEUED","data":null}`, want: OutputsResult{State: StatusQueued}},
		{name: "running", response: `{"code":804,"msg":"APIKEY_TASK_IS_RUNNING","data":null}`, want: OutputsResult{State: StatusRunning}},
		{
			name:     "failed",
			response: `{"code":805,"msg":"APIKEY_TASK_STATUS_ERROR","data":{"failedReason":{"node_id":"192","node_name":"UNETLoader","exception_message":"Value not in list: unet_name: 'x.safetensors' not in []","traceback":"..."}}}`,
			want: OutputsResult{State: StatusFailed, Message: "APIKEY_TASK_STATUS_ERROR", Failure: &FailedReason{
				NodeID: "192", NodeName: "UNETLoader", ExceptionMessage: "Value not in list: unet_name: 'x.safetensors' not in []",
			}},
		},
		{
			name:     "success",
			response: `{"code":0,"msg":"success","data":[{"fileUrl":"https://cdn.example/out.mp4","fileType":"mp4","taskCostTime":"272","nodeId":"264","consumeCoins":"120"}]}`,
			want: OutputsResult{State: StatusSuccess, Outputs: []Output{{
				FileURL: "https://cdn.example/out.mp4", FileType: "mp4", NodeID: "264", TaskCostTime: "272", ConsumeCoins: "120",
			}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestServer(t, func(path string, body map[string]any, _ *http.Request) string {
				require.Equal(t, "/task/openapi/outputs", path)
				assert.Equal(t, "77", body["taskId"])
				return tt.response
			})
			result, err := client.TaskOutputs(context.Background(), "77")
			require.NoError(t, err)
			assert.Equal(t, tt.want, *result)
		})
	}
}

func TestClientWorkflowJSONAndAccount(t *testing.T) {
	client := newTestServer(t, func(path string, body map[string]any, _ *http.Request) string {
		switch path {
		case "/api/openapi/getJsonApiFormat":
			assert.Equal(t, "123456789", body["workflowId"])
			return `{"code":0,"msg":"SUCCESS","data":{"prompt":"{\"3\":{\"class_type\":\"SaveImage\",\"inputs\":{}}}"}}`
		case "/uc/openapi/accountStatus":
			assert.Equal(t, "key-123", body["apikey"], "accountStatus spells the key field in lower case")
			assert.NotContains(t, body, "apiKey")
			return `{"code":0,"msg":"success","data":{"remainCoins":"4210","currentTaskCounts":"1","remainMoney":"12.5","currency":"CNY","apiType":"NORMAL"}}`
		}
		t.Fatalf("unexpected path %s", path)
		return ""
	})
	workflow, err := client.WorkflowAPIJSON(context.Background(), "123456789")
	require.NoError(t, err)
	assert.JSONEq(t, `{"3":{"class_type":"SaveImage","inputs":{}}}`, workflow)

	account, err := client.Account(context.Background())
	require.NoError(t, err)
	assert.Equal(t, AccountStatus{RemainCoins: "4210", RemainMoney: "12.5", Currency: "CNY", CurrentTaskCounts: "1", APIType: "NORMAL"}, *account)
}

func TestParseLink(t *testing.T) {
	tests := []struct {
		link string
		want LinkInfo
		ok   bool
	}{
		{link: "https://www.runninghub.ai/workflow/2108242199912755201", want: LinkInfo{ID: "2108242199912755201", Kind: "workflow"}, ok: true},
		{link: "https://www.runninghub.cn/#/workflow/1904136902449209346?source=x", want: LinkInfo{ID: "1904136902449209346", Kind: "workflow"}, ok: true},
		{link: "https://www.runninghub.ai/post/1987654321098765432", want: LinkInfo{ID: "1987654321098765432", Kind: "post"}, ok: true},
		{link: "https://www.runninghub.ai/ai-detail/1877265245566922800", want: LinkInfo{ID: "1877265245566922800", Kind: "app"}, ok: true},
		{link: " 2108242199912755201 ", want: LinkInfo{ID: "2108242199912755201", Kind: "workflow"}, ok: true},
		{link: "https://example.com/nothing", ok: false},
	}
	for _, tt := range tests {
		got, ok := ParseLink(tt.link)
		assert.Equal(t, tt.ok, ok, tt.link)
		assert.Equal(t, tt.want, got, tt.link)
	}
}
