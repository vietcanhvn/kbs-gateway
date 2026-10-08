// Package runninghub serves /v1/images/generations and /v1/images/edits for
// registered RunningHub workflows. The workflow runs as a RunningHub task;
// this adaptor waits for it so the image endpoints stay synchronous.
package runninghub

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/mediaworkflow"
	rh "github.com/QuantumNous/new-api/pkg/mediaworkflow/runninghub"
	"github.com/QuantumNous/new-api/relay/channel"
	taskrh "github.com/QuantumNous/new-api/relay/channel/task/runninghub"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	imageWaitTimeout  = 10 * time.Minute
	imagePollInterval = 3 * time.Second
)

type Adaptor struct {
	// Replaceable in tests.
	httpClient   *http.Client
	readMedia    taskrh.MediaReader
	pollInterval time.Duration
	fetchImage   func(url string) (string, error)
}

// imageJob is the converted request carried from ConvertImageRequest to
// DoRequest.
type imageJob struct {
	Model          string   `json:"model"`
	Prompt         string   `json:"prompt"`
	NegativePrompt string   `json:"negative_prompt,omitempty"`
	Images         []string `json:"images,omitempty"`
	Width          int      `json:"width,omitempty"`
	Height         int      `json:"height,omitempty"`
	Ratio          string   `json:"ratio,omitempty"`
	Seed           int64    `json:"seed,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
}

type imageResult struct {
	URLs []string `json:"urls"`
}

func (a *Adaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return strings.TrimRight(info.ChannelBaseUrl, "/") + "/task/openapi/create", nil
}

func (a *Adaptor) SetupRequestHeader(_ *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertImageRequest(_ *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	if info.RelayMode != relayconstant.RelayModeImagesGenerations && info.RelayMode != relayconstant.RelayModeImagesEdits {
		return nil, fmt.Errorf("unsupported runninghub image relay mode: %d", info.RelayMode)
	}
	images, err := imageReferences(request)
	if err != nil {
		return nil, err
	}
	job := imageJob{
		Model:          info.UpstreamModelName,
		Prompt:         request.Prompt,
		Images:         images,
		Width:          request.Width,
		Height:         request.Height,
		ResponseFormat: request.ResponseFormat,
	}
	if job.Width == 0 && job.Height == 0 {
		job.Width, job.Height = mediaworkflow.ParseSize(request.Size)
	}
	if ratio, ok := request.Metadata["ratio"].(string); ok {
		job.Ratio = ratio
	}
	if negative, ok := request.Metadata["negative_prompt"].(string); ok {
		job.NegativePrompt = negative
	}
	if seed, ok := request.Metadata["seed"].(float64); ok && seed > 0 {
		job.Seed = int64(seed)
	}
	return job, nil
}

func imageReferences(request dto.ImageRequest) ([]string, error) {
	images := make([]string, 0)
	for _, raw := range [][]byte{request.Image, request.Images} {
		text := strings.TrimSpace(string(raw))
		if text == "" || text == "null" {
			continue
		}
		var single string
		if common.Unmarshal(raw, &single) == nil {
			if strings.TrimSpace(single) != "" {
				images = append(images, strings.TrimSpace(single))
			}
			continue
		}
		var many []any
		if err := common.Unmarshal(raw, &many); err != nil {
			return nil, fmt.Errorf("image must be a string or an array")
		}
		for _, item := range many {
			switch typed := item.(type) {
			case string:
				if strings.TrimSpace(typed) != "" {
					images = append(images, strings.TrimSpace(typed))
				}
			case map[string]any:
				if url, ok := typed["url"].(string); ok && strings.TrimSpace(url) != "" {
					images = append(images, strings.TrimSpace(url))
				}
			}
		}
	}
	return images, nil
}

// DoRequest submits the workflow and waits for its result. Failures come
// back as an OpenAI-style error response so the relay's error handling
// (refund, status mapping) applies unchanged.
func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	body, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}
	var job imageJob
	if err := common.Unmarshal(body, &job); err != nil {
		return nil, err
	}
	c.Set("runninghub_response_format", job.ResponseFormat)
	workflow, err := taskrh.LoadWorkflow(job.Model)
	if err != nil {
		if errors.Is(err, model.ErrMediaWorkflowNotFound) {
			return errorResponse(http.StatusBadRequest, "model_not_found", fmt.Sprintf("model %s is not an enabled RunningHub workflow", job.Model)), nil
		}
		return nil, err
	}
	httpClient := a.httpClient
	if httpClient == nil {
		if httpClient, err = service.GetHttpClientWithProxy(info.ChannelSetting.Proxy); err != nil {
			return nil, err
		}
	}
	client := rh.New(info.ChannelBaseUrl, info.ApiKey, httpClient)
	reader := a.readMedia
	if reader == nil {
		reader = taskrh.ReadMediaInput
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), imageWaitTimeout)
	defer cancel()

	task, err := workflow.BuildTask(ctx, client, taskrh.Inputs{
		Prompt: job.Prompt, NegativePrompt: job.NegativePrompt, Images: job.Images,
		Width: job.Width, Height: job.Height, Ratio: job.Ratio, Seed: job.Seed,
	}, reader)
	if err != nil {
		return failureResponse(err), nil
	}
	created, err := client.CreateTask(ctx, task)
	if err != nil {
		return failureResponse(err), nil
	}
	interval := a.pollInterval
	if interval <= 0 {
		interval = imagePollInterval
	}
	for {
		select {
		case <-ctx.Done():
			_ = client.CancelTask(context.Background(), created.TaskID)
			return errorResponse(http.StatusGatewayTimeout, "runninghub_timeout", "RunningHub did not finish the image in time"), nil
		case <-time.After(interval):
		}
		status, err := client.TaskStatus(ctx, created.TaskID)
		if err != nil {
			var apiErr *rh.APIError
			if errors.As(err, &apiErr) && !apiErr.Retryable() {
				return failureResponse(err), nil
			}
			continue
		}
		if status != rh.StatusSuccess && status != rh.StatusFailed {
			continue
		}
		outputs, err := client.TaskOutputs(ctx, created.TaskID)
		if err != nil {
			return failureResponse(err), nil
		}
		if outputs.State == rh.StatusRunning || outputs.State == rh.StatusQueued {
			continue
		}
		if outputs.State == rh.StatusFailed {
			return errorResponse(http.StatusBadGateway, "runninghub_task_failed", taskrh.DescribeFailure(outputs)), nil
		}
		picked := taskrh.PickOutputs(outputs.Outputs, mediaworkflow.KindImage, workflow.OutputNodes)
		urls := make([]string, 0, len(picked))
		for _, output := range picked {
			if taskrh.OutputKind(output.FileType) == mediaworkflow.KindImage {
				urls = append(urls, output.FileURL)
			}
		}
		if len(urls) == 0 {
			return errorResponse(http.StatusBadGateway, "runninghub_no_output", "RunningHub task finished without an image output"), nil
		}
		data, _ := common.Marshal(imageResult{URLs: urls})
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(data))}, nil
	}
}

func failureResponse(err error) *http.Response {
	var requestErr *mediaworkflow.RequestError
	if errors.As(err, &requestErr) {
		return errorResponse(http.StatusBadRequest, "invalid_request", requestErr.Error())
	}
	var apiErr *rh.APIError
	if errors.As(err, &apiErr) {
		status := http.StatusBadGateway
		if apiErr.Retryable() {
			status = http.StatusTooManyRequests
		}
		return errorResponse(status, "runninghub_submit_failed", apiErr.Error())
	}
	return errorResponse(http.StatusBadGateway, "runninghub_error", err.Error())
}

func errorResponse(status int, code, message string) *http.Response {
	body, _ := common.Marshal(map[string]any{"error": map[string]any{"message": message, "type": "upstream_error", "code": code}})
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, _ *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	var result imageResult
	if err := common.DecodeJson(resp.Body, &result); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	wantsBase64 := !strings.EqualFold(c.GetString("runninghub_response_format"), "url")
	fetch := a.fetchImage
	if fetch == nil {
		fetch = func(url string) (string, error) {
			_, data, err := service.GetImageFromUrl(url)
			return data, err
		}
	}
	response := dto.ImageResponse{Created: time.Now().Unix()}
	for _, url := range result.URLs {
		if !wantsBase64 {
			response.Data = append(response.Data, dto.ImageData{Url: url})
			continue
		}
		b64, err := fetch(url)
		if err != nil {
			return nil, types.NewOpenAIError(fmt.Errorf("download RunningHub image: %w", err), types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		response.Data = append(response.Data, dto.ImageData{B64Json: b64})
	}
	payload, err := common.Marshal(response)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(payload)
	return &dto.Usage{PromptTokens: 1, TotalTokens: 1}, nil
}

func (a *Adaptor) ConvertOpenAIRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("runninghub channel only serves image generation and video tasks")
}

func (a *Adaptor) ConvertRerankRequest(*gin.Context, int, dto.RerankRequest) (any, error) {
	return nil, errors.New("runninghub channel does not support rerank")
}

func (a *Adaptor) ConvertEmbeddingRequest(*gin.Context, *relaycommon.RelayInfo, dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("runninghub channel does not support embeddings")
}

func (a *Adaptor) ConvertAudioRequest(*gin.Context, *relaycommon.RelayInfo, dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("runninghub channel does not support the audio endpoints; use /v1/video/generations")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(*gin.Context, *relaycommon.RelayInfo, dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("runninghub channel does not support responses")
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("runninghub channel does not support claude requests")
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("runninghub channel does not support gemini requests")
}

func (a *Adaptor) GetModelList() []string { return []string{} }

func (a *Adaptor) GetChannelName() string { return taskrh.ChannelName }

func (a *Adaptor) GetCapabilities() []string { return []string{"image"} }

var _ channel.Adaptor = (*Adaptor)(nil)
