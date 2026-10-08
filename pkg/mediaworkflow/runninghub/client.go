// Package runninghub is a small client for the RunningHub (cloud ComfyUI)
// workflow OpenAPI: upload input files, create a workflow task with node
// overrides, poll it, read its outputs, cancel it, fetch a workflow's API
// JSON and read the account status.
//
// Reference: https://www.runninghub.ai/runninghub-api-doc-en/ (mirrored at
// https://s.apifox.cn/apidoc/docs-site/5441421/). Every call is a POST with
// the API key in the body; the docs also list a Bearer header, which is sent
// as well.
package runninghub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const DefaultBaseURL = "https://www.runninghub.ai"

// Task states returned by RunningHub.
const (
	StatusQueued  = "QUEUED"
	StatusRunning = "RUNNING"
	StatusSuccess = "SUCCESS"
	StatusFailed  = "FAILED"
)

// Documented RunningHub error codes (doc-6913925).
const (
	CodeParamsInvalid               = 301
	CodeWorkflowNotExists           = 380
	CodeTokenInvalid                = 412
	CodeInstanceMaxed               = 415
	CodeNotEnoughWallet             = 416
	CodeQueueMaxed                  = 421
	CodeTaskNotFound                = 423
	CodeValidatePromptFailed        = 433
	CodeExclusiveInstanceGone       = 435
	CodeExclusiveRequired           = 436
	CodeFreeUserUnsupported         = 801
	CodeUnauthorized                = 802
	CodeInvalidNodeInfo             = 803
	CodeTaskRunning                 = 804
	CodeTaskFailed                  = 805
	CodeUserNotFound                = 806
	CodeAPITaskNotFound             = 807
	CodeUploadFailed                = 808
	CodeFileTooLarge                = 809
	CodeWorkflowNotSavedOrRun       = 810
	CodeCorpKeyInvalid              = 811
	CodeCorpInsufficientFunds       = 812
	CodeTaskQueued                  = 813
	CodeWebappNotExists             = 901
	MaxUploadBytes            int64 = 30 * 1024 * 1024
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL, apiKey string, httpClient *http.Client) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{BaseURL: baseURL, APIKey: strings.TrimSpace(apiKey), HTTP: httpClient}
}

// APIError is a non-success RunningHub envelope (code != 0).
type APIError struct {
	Op   string
	Code int
	Msg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("RunningHub %s failed (code %d): %s", e.Op, e.Code, Explain(e.Code, e.Msg))
}

// Retryable reports whether the same request may succeed later.
func (e *APIError) Retryable() bool {
	return e.Code == CodeInstanceMaxed || e.Code == CodeQueueMaxed || e.Code == CodeTaskQueued || e.Code == CodeTaskRunning
}

// Explain turns a RunningHub error code into an admin-readable message.
func Explain(code int, msg string) string {
	msg = strings.TrimSpace(msg)
	hint := ""
	switch code {
	case CodeParamsInvalid:
		hint = "invalid parameters"
	case CodeWorkflowNotExists:
		hint = "workflow not found for this API key; clone it into the RunningHub account that owns the key and use the clone's workflow ID"
	case CodeTokenInvalid, CodeUnauthorized, CodeCorpKeyInvalid, CodeUserNotFound:
		hint = "RunningHub API key is invalid"
	case CodeInstanceMaxed:
		hint = "RunningHub GPUs are busy, retry in 1-2 minutes"
	case CodeNotEnoughWallet, CodeCorpInsufficientFunds:
		hint = "RunningHub account balance is not enough"
	case CodeQueueMaxed:
		hint = "RunningHub queue is full (too many concurrent tasks for this account)"
	case CodeValidatePromptFailed:
		hint = "workflow validation failed on RunningHub (often a missing model file or custom node, or a value not accepted by a node)"
	case CodeExclusiveInstanceGone, CodeExclusiveRequired:
		hint = "the GPU type this workflow needs is not available for the key; try instance type \"plus\" (48 GB)"
	case CodeFreeUserUnsupported:
		hint = "free RunningHub accounts cannot use the API; a paid membership is required"
	case CodeInvalidNodeInfo:
		hint = "a mapped node ID / field name does not exist in the workflow"
	case CodeUploadFailed:
		hint = "upload to RunningHub failed"
	case CodeFileTooLarge:
		hint = "file is larger than RunningHub's 30 MB upload limit"
	case CodeWorkflowNotSavedOrRun:
		hint = "the workflow must be saved and run once successfully on the RunningHub website before the API can run it"
	case CodeTaskNotFound, CodeAPITaskNotFound:
		hint = "task not found"
	case CodeWebappNotExists:
		hint = "AI app not found"
	}
	switch {
	case hint == "":
		return msg
	case msg == "" || strings.EqualFold(msg, hint):
		return hint
	default:
		return hint + " (" + msg + ")"
	}
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (c *Client) post(ctx context.Context, op, path string, payload map[string]any) (*envelope, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("RunningHub API key is not configured")
	}
	if _, ok := payload["apikey"]; !ok {
		payload["apiKey"] = c.APIKey
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, op)
}

func (c *Client) do(req *http.Request, op string) (*envelope, error) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("RunningHub %s request failed: %w", op, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("RunningHub %s read failed: %w", op, err)
	}
	var env envelope
	if err := common.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("RunningHub %s returned HTTP %d with an unreadable body: %s", op, resp.StatusCode, truncate(string(raw), 300))
	}
	if resp.StatusCode >= http.StatusBadRequest && env.Code == 0 {
		env.Code = resp.StatusCode
	}
	return &env, nil
}

func success(env *envelope) bool {
	// The outputs reference page shows code 200 while every official sample
	// checks code 0; accept both.
	return env.Code == 0 || env.Code == 200
}

// Upload sends one input file and returns the name to put in a loader node's
// field (e.g. "api/<hash>.png").
func (c *Client) Upload(ctx context.Context, filename string, data []byte) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("RunningHub API key is not configured")
	}
	if int64(len(data)) > MaxUploadBytes {
		return "", &APIError{Op: "upload", Code: CodeFileTooLarge}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("apiKey", c.APIKey)
	_ = writer.WriteField("fileType", "input")
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/task/openapi/upload", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	env, err := c.do(req, "upload")
	if err != nil {
		return "", err
	}
	if !success(env) {
		return "", &APIError{Op: "upload", Code: env.Code, Msg: env.Msg}
	}
	var uploaded struct {
		FileName string `json:"fileName"`
	}
	if err := common.Unmarshal(env.Data, &uploaded); err != nil || strings.TrimSpace(uploaded.FileName) == "" {
		return "", fmt.Errorf("RunningHub upload returned no fileName")
	}
	return uploaded.FileName, nil
}

type NodeInfo struct {
	NodeID     string `json:"nodeId"`
	FieldName  string `json:"fieldName"`
	FieldValue any    `json:"fieldValue"`
}

type CreateTaskRequest struct {
	WorkflowID   string
	NodeInfoList []NodeInfo
	InstanceType string
	WebhookURL   string
}

type CreateTaskResult struct {
	TaskID     string `json:"taskId"`
	TaskStatus string `json:"taskStatus"`
	PromptTips string `json:"promptTips,omitempty"`
}

// CreateTask starts a workflow run (POST /task/openapi/create).
func (c *Client) CreateTask(ctx context.Context, request CreateTaskRequest) (*CreateTaskResult, error) {
	if strings.TrimSpace(request.WorkflowID) == "" {
		return nil, fmt.Errorf("RunningHub workflow ID is empty")
	}
	payload := map[string]any{"workflowId": strings.TrimSpace(request.WorkflowID)}
	if len(request.NodeInfoList) > 0 {
		payload["nodeInfoList"] = request.NodeInfoList
	}
	if request.InstanceType != "" {
		payload["instanceType"] = request.InstanceType
	}
	if request.WebhookURL != "" {
		payload["webhookUrl"] = request.WebhookURL
	}
	env, err := c.post(ctx, "create task", "/task/openapi/create", payload)
	if err != nil {
		return nil, err
	}
	if !success(env) {
		return nil, &APIError{Op: "create task", Code: env.Code, Msg: env.Msg}
	}
	var data struct {
		TaskID     json.RawMessage `json:"taskId"`
		TaskStatus string          `json:"taskStatus"`
		PromptTips string          `json:"promptTips"`
	}
	if err := common.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("RunningHub create task: %w", err)
	}
	taskID := rawID(data.TaskID)
	if taskID == "" {
		return nil, fmt.Errorf("RunningHub create task returned no taskId")
	}
	return &CreateTaskResult{TaskID: taskID, TaskStatus: data.TaskStatus, PromptTips: data.PromptTips}, nil
}

// TaskStatus returns QUEUED, RUNNING, SUCCESS or FAILED.
func (c *Client) TaskStatus(ctx context.Context, taskID string) (string, error) {
	env, err := c.post(ctx, "task status", "/task/openapi/status", map[string]any{"taskId": taskID})
	if err != nil {
		return "", err
	}
	if !success(env) {
		return "", &APIError{Op: "task status", Code: env.Code, Msg: env.Msg}
	}
	var status string
	if err := common.Unmarshal(env.Data, &status); err != nil {
		return "", fmt.Errorf("RunningHub task status: %w", err)
	}
	return strings.ToUpper(strings.TrimSpace(status)), nil
}

type Output struct {
	FileURL      string `json:"fileUrl"`
	FileType     string `json:"fileType"`
	NodeID       string `json:"nodeId"`
	TaskCostTime string `json:"taskCostTime,omitempty"`
	ConsumeCoins string `json:"consumeCoins,omitempty"`
	ConsumeMoney string `json:"consumeMoney,omitempty"`
}

type FailedReason struct {
	NodeID           string `json:"node_id,omitempty"`
	NodeName         string `json:"node_name,omitempty"`
	ExceptionMessage string `json:"exception_message,omitempty"`
}

// OutputsResult is the outcome of /task/openapi/outputs: State is one of the
// Status* constants.
type OutputsResult struct {
	State   string        `json:"state"`
	Outputs []Output      `json:"outputs,omitempty"`
	Failure *FailedReason `json:"failure,omitempty"`
	Message string        `json:"message,omitempty"`
}

// TaskOutputs reads the outputs; while the task is not finished RunningHub
// answers with code 804 (running) or 813 (queued), and 805 when it failed.
func (c *Client) TaskOutputs(ctx context.Context, taskID string) (*OutputsResult, error) {
	env, err := c.post(ctx, "task outputs", "/task/openapi/outputs", map[string]any{"taskId": taskID})
	if err != nil {
		return nil, err
	}
	switch {
	case env.Code == CodeTaskRunning:
		return &OutputsResult{State: StatusRunning}, nil
	case env.Code == CodeTaskQueued:
		return &OutputsResult{State: StatusQueued}, nil
	case env.Code == CodeTaskFailed:
		return &OutputsResult{State: StatusFailed, Failure: parseFailedReason(env.Data), Message: env.Msg}, nil
	case !success(env):
		return nil, &APIError{Op: "task outputs", Code: env.Code, Msg: env.Msg}
	}
	var items []struct {
		FileURL      string `json:"fileUrl"`
		FileType     string `json:"fileType"`
		NodeID       any    `json:"nodeId"`
		TaskCostTime any    `json:"taskCostTime"`
		ConsumeCoins any    `json:"consumeCoins"`
		ConsumeMoney any    `json:"consumeMoney"`
	}
	if len(bytes.TrimSpace(env.Data)) > 0 && string(bytes.TrimSpace(env.Data)) != "null" {
		if err := common.Unmarshal(env.Data, &items); err != nil {
			return nil, fmt.Errorf("RunningHub task outputs: %w", err)
		}
	}
	result := &OutputsResult{State: StatusSuccess}
	for _, item := range items {
		if strings.TrimSpace(item.FileURL) == "" {
			continue
		}
		result.Outputs = append(result.Outputs, Output{
			FileURL: strings.TrimSpace(item.FileURL), FileType: strings.ToLower(strings.TrimSpace(item.FileType)),
			NodeID: anyString(item.NodeID), TaskCostTime: anyString(item.TaskCostTime),
			ConsumeCoins: anyString(item.ConsumeCoins), ConsumeMoney: anyString(item.ConsumeMoney),
		})
	}
	return result, nil
}

func parseFailedReason(raw json.RawMessage) *FailedReason {
	var data struct {
		FailedReason json.RawMessage `json:"failedReason"`
	}
	if err := common.Unmarshal(raw, &data); err != nil || len(data.FailedReason) == 0 {
		return nil
	}
	reasonRaw := data.FailedReason
	var asText string
	if common.Unmarshal(reasonRaw, &asText) == nil {
		if strings.HasPrefix(strings.TrimSpace(asText), "{") {
			reasonRaw = []byte(asText)
		} else {
			return &FailedReason{ExceptionMessage: asText}
		}
	}
	var fields map[string]any
	if err := common.Unmarshal(reasonRaw, &fields); err != nil {
		return nil
	}
	return &FailedReason{
		NodeID:           anyString(fields["node_id"]),
		NodeName:         anyString(fields["node_name"]),
		ExceptionMessage: anyString(fields["exception_message"]),
	}
}

// CancelTask cancels a queued or running task.
func (c *Client) CancelTask(ctx context.Context, taskID string) error {
	env, err := c.post(ctx, "cancel task", "/task/openapi/cancel", map[string]any{"taskId": taskID})
	if err != nil {
		return err
	}
	if !success(env) {
		return &APIError{Op: "cancel task", Code: env.Code, Msg: env.Msg}
	}
	return nil
}

// WorkflowAPIJSON returns a workflow's API-format JSON
// (POST /api/openapi/getJsonApiFormat; data.prompt is a JSON string).
func (c *Client) WorkflowAPIJSON(ctx context.Context, workflowID string) (string, error) {
	env, err := c.post(ctx, "get workflow JSON", "/api/openapi/getJsonApiFormat", map[string]any{"workflowId": strings.TrimSpace(workflowID)})
	if err != nil {
		return "", err
	}
	if !success(env) {
		return "", &APIError{Op: "get workflow JSON", Code: env.Code, Msg: env.Msg}
	}
	var data struct {
		Prompt json.RawMessage `json:"prompt"`
	}
	if err := common.Unmarshal(env.Data, &data); err != nil {
		return "", fmt.Errorf("RunningHub get workflow JSON: %w", err)
	}
	var text string
	if common.Unmarshal(data.Prompt, &text) == nil {
		return text, nil
	}
	if len(data.Prompt) == 0 {
		return "", fmt.Errorf("RunningHub returned an empty workflow")
	}
	return string(data.Prompt), nil
}

type AccountStatus struct {
	RemainCoins       string `json:"remain_coins"`
	RemainMoney       string `json:"remain_money"`
	Currency          string `json:"currency"`
	CurrentTaskCounts string `json:"current_task_counts"`
	APIType           string `json:"api_type"`
}

// Account reads POST /uc/openapi/accountStatus (this endpoint spells the key
// field "apikey").
func (c *Client) Account(ctx context.Context) (*AccountStatus, error) {
	env, err := c.post(ctx, "account status", "/uc/openapi/accountStatus", map[string]any{"apikey": c.APIKey})
	if err != nil {
		return nil, err
	}
	if !success(env) {
		return nil, &APIError{Op: "account status", Code: env.Code, Msg: env.Msg}
	}
	var data map[string]any
	if err := common.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("RunningHub account status: %w", err)
	}
	return &AccountStatus{
		RemainCoins: anyString(data["remainCoins"]), RemainMoney: anyString(data["remainMoney"]),
		Currency: anyString(data["currency"]), CurrentTaskCounts: anyString(data["currentTaskCounts"]),
		APIType: anyString(data["apiType"]),
	}, nil
}

// rawID keeps 19-digit snowflake IDs exact whether they arrive as a JSON
// string or a JSON number.
func rawID(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	if strings.HasPrefix(text, "\"") {
		var value string
		if common.Unmarshal(raw, &value) == nil {
			return strings.TrimSpace(value)
		}
		return ""
	}
	return text
}

func anyString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", typed), "0"), ".")
	default:
		return fmt.Sprint(typed)
	}
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
