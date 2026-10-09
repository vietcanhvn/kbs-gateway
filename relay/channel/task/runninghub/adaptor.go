// Package runninghub runs registered media workflows (model.MediaWorkflow)
// on RunningHub through the DC-Media task API (/v1/video/generations).
package runninghub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/mediaworkflow"
	rh "github.com/QuantumNous/new-api/pkg/mediaworkflow/runninghub"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const ChannelName = "runninghub"

const defaultBilledSeconds = 5

type TaskAdaptor struct {
	taskcommon.BaseBilling
	baseURL string
	apiKey  string
	// readMedia and httpClient are replaceable in tests.
	readMedia  MediaReader
	httpClient *http.Client
}

var _ channel.TaskAdaptor = (*TaskAdaptor)(nil)
var _ channel.OpenAIVideoConverter = (*TaskAdaptor)(nil)
var _ channel.TaskCanceller = (*TaskAdaptor)(nil)

// pollResult is what FetchTask hands to ParseTaskResult (and what is kept in
// task.Data, minus the URL).
type pollResult struct {
	RHTaskID     string `json:"rh_task_id"`
	Status       string `json:"status"`
	URL          string `json:"url,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Reason       string `json:"reason,omitempty"`
	CostTime     string `json:"cost_time,omitempty"`
	ConsumeCoins string `json:"consume_coins,omitempty"`
}

type taskMetadata struct {
	NegativePrompt  string   `json:"negative_prompt,omitempty"`
	LastFrameImage  string   `json:"last_frame_image,omitempty"`
	ReferenceImages []string `json:"reference_images,omitempty"`
	ReferenceVideos []string `json:"reference_videos,omitempty"`
	ReferenceAudios []string `json:"reference_audios,omitempty"`
	Width           int      `json:"width,omitempty"`
	Height          int      `json:"height,omitempty"`
	Ratio           string   `json:"ratio,omitempty"`
	AspectRatio     string   `json:"aspect_ratio,omitempty"`
	Resolution      string   `json:"resolution,omitempty"`
	Duration        int      `json:"duration,omitempty"`
	Seed            int64    `json:"seed,omitempty"`
	// Timeline workflows (long take in segments).
	Segments  []mediaworkflow.TimelineSegmentSpec `json:"segments,omitempty"`
	AudioLock *bool                               `json:"audio_lock,omitempty"`
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info.ChannelMeta != nil {
		a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	}
	if a.baseURL == "" {
		a.baseURL = rh.DefaultBaseURL
	}
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}
	if _, err := LoadWorkflow(info.UpstreamModelName); err != nil {
		if errors.Is(err, model.ErrMediaWorkflowNotFound) {
			return service.TaskErrorWrapperLocal(fmt.Errorf("model %s is not an enabled RunningHub workflow", info.UpstreamModelName), "model_not_found", http.StatusBadRequest)
		}
		return service.TaskErrorWrapperLocal(err, "runninghub_configuration_error", http.StatusInternalServerError)
	}
	return nil
}

// EstimateBilling multiplies the model price by the requested seconds for
// workflows whose mapping says billing "per_second"; otherwise the model
// price is charged once per task.
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	workflow, err := LoadWorkflow(info.UpstreamModelName)
	if err != nil || workflow.Mapping.Billing != "per_second" {
		return nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	inputs, err := taskInputs(req)
	if err != nil {
		return nil
	}
	billing := map[string]float64{}
	// Timeline (long take): the video is as long as its segments together.
	if seconds, ok, err := workflow.TimelineSeconds(inputs); ok {
		if err != nil {
			return nil
		}
		billing["seconds"] = float64(BilledSeconds(workflow.Mapping, int(math.Ceil(seconds)), 0))
	} else {
		billing["seconds"] = float64(BilledSeconds(workflow.Mapping, inputs.Duration, workflow.DefaultSeconds))
	}
	// Workflows that render the requested resolution: the per-second price is
	// the workflow default (720p); larger tiers cost more by pixel count.
	// Smaller tiers keep the 720p price (RunningHub time does not shrink as fast).
	if scale := workflow.Mapping.ResolutionScale(inputs.Resolution); scale > 1 {
		billing["resolution"] = math.Round(scale*100) / 100
	}
	return billing
}

// BilledSeconds is the duration actually rendered (clamped to the duration
// input's bounds), bounded by MaxTaskDurationSeconds. A request without a
// duration renders the workflow's own default (workflowDefault), so that is
// what gets billed; defaultBilledSeconds is only the last resort.
func BilledSeconds(mapping mediaworkflow.InputMapping, requested int, workflowDefault float64) int {
	seconds := float64(requested)
	if seconds <= 0 {
		seconds = workflowDefault
	}
	if seconds <= 0 {
		seconds = defaultBilledSeconds
	}
	for _, binding := range mapping.Inputs {
		if binding.Role != mediaworkflow.RoleDuration {
			continue
		}
		if binding.Min != nil && seconds < *binding.Min {
			seconds = *binding.Min
		}
		if binding.Max != nil && seconds > *binding.Max {
			seconds = *binding.Max
		}
	}
	if seconds > relaycommon.MaxTaskDurationSeconds {
		seconds = relaycommon.MaxTaskDurationSeconds
	}
	if seconds < 1 {
		seconds = 1
	}
	return int(seconds + 0.5)
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + "/task/openapi/create", nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) client(proxy string) (*rh.Client, error) {
	httpClient := a.httpClient
	if httpClient == nil {
		var err error
		httpClient, err = service.GetHttpClientWithProxy(proxy)
		if err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
	}
	return rh.New(a.baseURL, a.apiKey, httpClient), nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	workflow, err := LoadWorkflow(info.UpstreamModelName)
	if err != nil {
		return nil, err
	}
	inputs, err := taskInputs(req)
	if err != nil {
		return nil, localRequestError(err)
	}
	client, err := a.client(info.ChannelSetting.Proxy)
	if err != nil {
		return nil, err
	}
	reader := a.readMedia
	if reader == nil {
		reader = ReadMediaInput
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	task, err := workflow.BuildTask(ctx, client, inputs, reader)
	if err != nil {
		return nil, classifyBuildError(err)
	}
	payload := map[string]any{
		"apiKey":       a.apiKey,
		"workflowId":   task.WorkflowID,
		"nodeInfoList": task.NodeInfoList,
	}
	if task.InstanceType != "" {
		payload["instanceType"] = task.InstanceType
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	c.Set(contextKeyOutput, UpstreamTaskID("", workflow.MediaKind, workflow.OutputNodes))
	return bytes.NewReader(body), nil
}

const contextKeyOutput = "runninghub_output_preference"

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *taskdto.TaskError) {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	var created struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			TaskStatus string `json:"taskStatus"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &created); err != nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("RunningHub create task returned an unreadable body"), "runninghub_bad_response", http.StatusBadGateway)
	}
	if created.Code != 0 && created.Code != 200 {
		apiErr := &rh.APIError{Op: "create task", Code: created.Code, Msg: created.Msg}
		return "", nil, service.TaskErrorWrapper(apiErr, "runninghub_submit_failed", submitErrorStatus(created.Code))
	}
	taskID := createdTaskID(body)
	if taskID == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("RunningHub create task returned no taskId"), "runninghub_bad_response", http.StatusBadGateway)
	}
	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)

	_, kind, nodes := ParseUpstreamTaskID(c.GetString(contextKeyOutput))
	data, _ := common.Marshal(pollResult{RHTaskID: taskID, Status: "queued", Kind: kind})
	return UpstreamTaskID(taskID, kind, nodes), data, nil
}

// createdTaskID reads data.taskId exactly (19-digit IDs lose precision as float64).
func createdTaskID(body []byte) string {
	var envelope struct {
		Data struct {
			TaskID json.RawMessage `json:"taskId"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(string(envelope.Data.TaskID)), "\"")
}

func submitErrorStatus(code int) int {
	switch code {
	case rh.CodeInstanceMaxed, rh.CodeQueueMaxed:
		return http.StatusTooManyRequests
	case rh.CodeNotEnoughWallet, rh.CodeCorpInsufficientFunds, rh.CodeTokenInvalid, rh.CodeUnauthorized,
		rh.CodeCorpKeyInvalid, rh.CodeFreeUserUnsupported:
		return http.StatusServiceUnavailable
	case rh.CodeInvalidNodeInfo, rh.CodeValidatePromptFailed, rh.CodeWorkflowNotExists, rh.CodeWorkflowNotSavedOrRun:
		return http.StatusInternalServerError
	}
	return http.StatusBadGateway
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	upstreamID, _ := body["task_id"].(string)
	taskID, kind, nodes := ParseUpstreamTaskID(upstreamID)
	if taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	httpClient := a.httpClient
	if httpClient == nil {
		var err error
		if httpClient, err = service.GetHttpClientWithProxy(proxy); err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
	}
	client := rh.New(baseURL, key, httpClient)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result := pollResult{RHTaskID: taskID, Kind: kind}
	status, err := client.TaskStatus(ctx, taskID)
	var apiErr *rh.APIError
	switch {
	case errors.As(err, &apiErr) && (apiErr.Code == rh.CodeTaskNotFound || apiErr.Code == rh.CodeAPITaskNotFound):
		result.Status = "failed"
		result.Reason = apiErr.Error()
	case err != nil:
		return nil, err
	case status == rh.StatusSuccess || status == rh.StatusFailed:
		outputs, err := client.TaskOutputs(ctx, taskID)
		if err != nil {
			return nil, err
		}
		applyOutputs(&result, outputs, kind, nodes)
	case status == rh.StatusRunning:
		result.Status = "running"
	default:
		result.Status = "queued"
	}
	data, err := common.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func applyOutputs(result *pollResult, outputs *rh.OutputsResult, kind string, nodes []string) {
	switch outputs.State {
	case rh.StatusFailed:
		result.Status = "failed"
		result.Reason = DescribeFailure(outputs)
		return
	case rh.StatusRunning:
		result.Status = "running"
		return
	case rh.StatusQueued:
		result.Status = "queued"
		return
	}
	picked := PickOutputs(outputs.Outputs, kind, nodes)
	if len(picked) == 0 {
		result.Status = "failed"
		result.Reason = "RunningHub task finished without a usable output file"
		return
	}
	result.Status = "succeeded"
	result.URL = picked[0].FileURL
	result.Kind = OutputKind(picked[0].FileType)
	result.CostTime = picked[0].TaskCostTime
	result.ConsumeCoins = picked[0].ConsumeCoins
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var result pollResult
	if err := common.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal runninghub task result failed: %w", err)
	}
	info := &relaycommon.TaskInfo{TaskID: result.RHTaskID}
	switch result.Status {
	case "succeeded":
		info.Status = model.TaskStatusSuccess
		info.Progress = taskcommon.ProgressComplete
		info.RemoteUrl = result.URL
	case "failed":
		info.Status = model.TaskStatusFailure
		info.Progress = taskcommon.ProgressComplete
		info.Reason = firstNonEmpty(result.Reason, "RunningHub task failed")
	case "running":
		info.Status = model.TaskStatusInProgress
		info.Progress = "50%"
	default:
		info.Status = model.TaskStatusQueued
		info.Progress = taskcommon.ProgressQueued
	}
	return info, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var result pollResult
	_ = common.Unmarshal(originTask.Data, &result)
	video := dto.NewOpenAIVideo()
	video.ID = originTask.TaskID
	video.TaskID = originTask.TaskID
	video.Status = originTask.Status.ToVideoStatus()
	video.SetProgressStr(originTask.Progress)
	video.CreatedAt = originTask.CreatedAt
	video.CompletedAt = originTask.UpdatedAt
	video.Model = originTask.Properties.OriginModelName
	if originTask.Status == model.TaskStatusSuccess && originTask.PrivateData.UpstreamResultURL != "" {
		resultURL := taskcommon.BuildPublicProxyURL(originTask.TaskID)
		video.SetMetadata("url", resultURL)
		if result.Kind == mediaworkflow.KindImage {
			video.SetMetadata("image_url", resultURL)
		} else {
			video.SetMetadata("video_url", resultURL)
		}
	}
	if originTask.Status == model.TaskStatusFailure {
		video.Error = &dto.OpenAIVideoError{Message: firstNonEmpty(originTask.FailReason, result.Reason), Code: "runninghub_task_failed"}
	}
	return common.Marshal(video)
}

func (a *TaskAdaptor) CancelTask(ctx context.Context, baseURL, key, upstreamTaskID, proxy string) error {
	taskID, _, _ := ParseUpstreamTaskID(upstreamTaskID)
	if taskID == "" {
		return channel.ErrTaskNotCancellable
	}
	httpClient := a.httpClient
	if httpClient == nil {
		var err error
		if httpClient, err = service.GetHttpClientWithProxy(proxy); err != nil {
			return fmt.Errorf("new proxy http client failed: %w", err)
		}
	}
	client := rh.New(baseURL, key, httpClient)
	status, err := client.TaskStatus(ctx, taskID)
	if err != nil {
		return err
	}
	if status == rh.StatusSuccess || status == rh.StatusFailed {
		return channel.ErrTaskNotCancellable
	}
	if err := client.CancelTask(ctx, taskID); err != nil {
		return err
	}
	// Report success only once RunningHub no longer runs the task.
	status, err = client.TaskStatus(ctx, taskID)
	if err != nil {
		return err
	}
	if status == rh.StatusQueued || status == rh.StatusRunning {
		return channel.ErrTaskCancellationUnsupported
	}
	if status == rh.StatusSuccess {
		return channel.ErrTaskNotCancellable
	}
	return nil
}

func (a *TaskAdaptor) GetModelList() []string { return []string{} }

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) GetCapabilities() []string {
	return []string{"image", "video", "audio"}
}

func (a *TaskAdaptor) OverridesSyncCapabilities() bool { return true }

// taskInputs gathers DC-Media task fields in the order KSB numbers them
// (@image1 = first image): top-level image, images[], metadata references,
// then content items.
func taskInputs(req relaycommon.TaskSubmitReq) (Inputs, error) {
	var metadata taskMetadata
	if err := taskcommon.UnmarshalMetadata(req.Metadata, &metadata); err != nil {
		return Inputs{}, err
	}
	inputs := Inputs{
		Prompt:         req.Prompt,
		NegativePrompt: metadata.NegativePrompt,
		LastFrame:      strings.TrimSpace(metadata.LastFrameImage),
		Duration:       firstPositive(req.Duration, metadata.Duration),
		Width:          firstPositive(req.Width, metadata.Width),
		Height:         firstPositive(req.Height, metadata.Height),
		Ratio:          firstNonEmpty(metadata.Ratio, metadata.AspectRatio),
		Resolution:     metadata.Resolution,
		Seed:           metadata.Seed,
		Segments:       metadata.Segments,
		AudioLock:      metadata.AudioLock,
	}
	if inputs.Seed <= 0 && req.Seed > 0 {
		inputs.Seed = int64(req.Seed)
	}
	if inputs.Width == 0 && inputs.Height == 0 {
		inputs.Width, inputs.Height = mediaworkflow.ParseSize(req.Size)
	}
	images := appendUnique(nil, req.Image)
	for _, image := range req.Images {
		images = appendUnique(images, image)
	}
	for _, image := range metadata.ReferenceImages {
		images = appendUnique(images, image)
	}
	videos := appendUnique(nil, metadata.ReferenceVideos...)
	audios := appendUnique(nil, metadata.ReferenceAudios...)
	for _, item := range req.Content {
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "image_url":
			value := contentURL(item.ImageURL)
			if strings.EqualFold(strings.TrimSpace(item.Role), "last_frame") {
				inputs.LastFrame = firstNonEmpty(inputs.LastFrame, value)
				continue
			}
			images = appendUnique(images, value)
		case "video_url":
			videos = appendUnique(videos, contentURL(item.VideoURL))
		case "audio_url":
			audios = appendUnique(audios, contentURL(item.AudioURL))
		}
	}
	inputs.Images, inputs.Videos, inputs.Audios = images, videos, audios
	return inputs, nil
}

func contentURL(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		text, _ := typed["url"].(string)
		return strings.TrimSpace(text)
	}
	return ""
}

func appendUnique(values []string, items ...string) []string {
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		duplicate := false
		for _, existing := range values {
			if existing == item {
				duplicate = true
				break
			}
		}
		if !duplicate {
			values = append(values, item)
		}
	}
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
