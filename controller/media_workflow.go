package controller

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/mediaworkflow"
	rh "github.com/QuantumNous/new-api/pkg/mediaworkflow/runninghub"
	taskrh "github.com/QuantumNous/new-api/relay/channel/task/runninghub"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// Admin API of the media workflow registry ("RunningHub workflows" page).

var mediaWorkflowModelNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type mediaWorkflowPayload struct {
	Id             int                          `json:"id"`
	ModelName      string                       `json:"model_name"`
	Title          string                       `json:"title"`
	SourceURL      string                       `json:"source_url"`
	WorkflowID     string                       `json:"workflow_id"`
	MediaKind      string                       `json:"media_kind"`
	Executor       string                       `json:"executor"`
	ApiJSON        *string                      `json:"api_json"`
	UiJSON         *string                      `json:"ui_json"`
	InputMapping   *mediaworkflow.InputMapping  `json:"input_mapping"`
	OutputNodes    []string                     `json:"output_nodes"`
	PromptTemplate mediaworkflow.PromptTemplate `json:"prompt_template"`
	Enabled        bool                         `json:"enabled"`
}

type mediaWorkflowView struct {
	Id             int                          `json:"id"`
	ModelName      string                       `json:"model_name"`
	Title          string                       `json:"title"`
	SourceURL      string                       `json:"source_url"`
	WorkflowID     string                       `json:"workflow_id"`
	MediaKind      string                       `json:"media_kind"`
	Executor       string                       `json:"executor"`
	ApiJSON        string                       `json:"api_json"`
	HasUiJSON      bool                         `json:"has_ui_json"`
	Analysis       *mediaworkflow.Analysis      `json:"analysis"`
	InputMapping   mediaworkflow.InputMapping   `json:"input_mapping"`
	OutputNodes    []string                     `json:"output_nodes"`
	PromptTemplate mediaworkflow.PromptTemplate `json:"prompt_template"`
	Enabled        bool                         `json:"enabled"`
	UpdatedTime    int64                        `json:"updated_time"`
}

func newMediaWorkflowView(record *model.MediaWorkflow) mediaWorkflowView {
	view := mediaWorkflowView{
		Id: record.Id, ModelName: record.ModelName, Title: record.Title, SourceURL: record.SourceURL,
		WorkflowID: record.WorkflowID, MediaKind: record.MediaKind, Executor: record.Executor,
		ApiJSON: record.ApiJSON, HasUiJSON: strings.TrimSpace(record.UiJSON) != "",
		Enabled: record.Enabled, UpdatedTime: record.UpdatedTime, OutputNodes: []string{},
	}
	if record.Analysis != "" {
		var analysis mediaworkflow.Analysis
		if common.UnmarshalJsonStr(record.Analysis, &analysis) == nil {
			analysis.Normalize()
			view.Analysis = &analysis
		}
	}
	_ = common.UnmarshalJsonStr(record.InputMapping, &view.InputMapping)
	_ = common.UnmarshalJsonStr(record.OutputNodes, &view.OutputNodes)
	_ = common.UnmarshalJsonStr(record.PromptTemplate, &view.PromptTemplate)
	return view
}

func ListMediaWorkflows(c *gin.Context) {
	items, err := model.ListMediaWorkflows()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func GetMediaWorkflow(c *gin.Context) {
	record, ok := mediaWorkflowFromParam(c)
	if !ok {
		return
	}
	common.ApiSuccess(c, newMediaWorkflowView(record))
}

func mediaWorkflowFromParam(c *gin.Context) (*model.MediaWorkflow, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "invalid id")
		return nil, false
	}
	record, err := model.GetMediaWorkflowByID(id)
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	return record, true
}

type mediaWorkflowAnalyzeRequest struct {
	ApiJSON string `json:"api_json"`
	UiJSON  string `json:"ui_json"`
}

type mediaWorkflowAnalyzeResponse struct {
	Analysis             *mediaworkflow.Analysis    `json:"analysis"`
	SuggestedMapping     mediaworkflow.InputMapping `json:"suggested_mapping"`
	SuggestedOutputNodes []string                   `json:"suggested_output_nodes"`
}

func analyzeMediaWorkflow(apiJSON, uiJSON string) (*mediaWorkflowAnalyzeResponse, error) {
	analysis, err := mediaworkflow.Analyze([]byte(apiJSON), []byte(uiJSON))
	if err != nil {
		return nil, err
	}
	return &mediaWorkflowAnalyzeResponse{
		Analysis:             analysis,
		SuggestedMapping:     mediaworkflow.SuggestMapping(analysis),
		SuggestedOutputNodes: mediaworkflow.SuggestOutputNodes(analysis),
	}, nil
}

// AnalyzeMediaWorkflow analyzes pasted/uploaded workflow JSON without saving.
func AnalyzeMediaWorkflow(c *gin.Context) {
	var req mediaWorkflowAnalyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := analyzeMediaWorkflow(req.ApiJSON, req.UiJSON)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

type mediaWorkflowFetchRequest struct {
	Link      string `json:"link"`
	ChannelID int    `json:"channel_id"`
}

// FetchMediaWorkflow reads a RunningHub link, extracts the workflow ID and,
// when a RunningHub channel with a key exists, downloads the API JSON.
func FetchMediaWorkflow(c *gin.Context) {
	var req mediaWorkflowFetchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	link, ok := rh.ParseLink(req.Link)
	if !ok {
		common.ApiErrorMsg(c, "could not find a RunningHub workflow ID in this link")
		return
	}
	response := gin.H{"link": link}
	client, channel, err := runningHubClientForAdmin(req.ChannelID)
	if err != nil {
		response["fetch_error"] = err.Error()
		common.ApiSuccess(c, response)
		return
	}
	response["channel_id"] = channel.Id
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	apiJSON, err := client.WorkflowAPIJSON(ctx, link.ID)
	if err != nil {
		response["fetch_error"] = err.Error()
		common.ApiSuccess(c, response)
		return
	}
	response["api_json"] = apiJSON
	if analyzed, err := analyzeMediaWorkflow(apiJSON, ""); err == nil {
		response["analyzed"] = analyzed
	} else {
		response["fetch_error"] = err.Error()
	}
	common.ApiSuccess(c, response)
}

func CreateMediaWorkflow(c *gin.Context) {
	saveMediaWorkflow(c, false)
}

func UpdateMediaWorkflow(c *gin.Context) {
	saveMediaWorkflow(c, true)
}

func saveMediaWorkflow(c *gin.Context, update bool) {
	var payload mediaWorkflowPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		common.ApiError(c, err)
		return
	}
	record := &model.MediaWorkflow{}
	if update {
		existing, err := model.GetMediaWorkflowByID(payload.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		record = existing
	}
	if err := applyMediaWorkflowPayload(record, payload); err != nil {
		common.ApiError(c, err)
		return
	}
	taken, err := model.IsMediaWorkflowModelNameTaken(record.Id, record.ModelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if taken {
		common.ApiErrorMsg(c, "model name is already used by another workflow")
		return
	}
	if update {
		err = record.Update()
	} else {
		err = record.Insert()
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, newMediaWorkflowView(record))
}

func applyMediaWorkflowPayload(record *model.MediaWorkflow, payload mediaWorkflowPayload) error {
	record.ModelName = strings.TrimSpace(payload.ModelName)
	if !mediaWorkflowModelNamePattern.MatchString(record.ModelName) {
		return errors.New("model name must be 1-128 letters, digits, '.', '_' or '-'")
	}
	record.Title = strings.TrimSpace(payload.Title)
	record.SourceURL = strings.TrimSpace(payload.SourceURL)
	record.WorkflowID = strings.TrimSpace(payload.WorkflowID)
	record.Executor = strings.TrimSpace(payload.Executor)
	if record.Executor == "" {
		record.Executor = model.MediaWorkflowExecutorRunningHub
	}
	if record.Executor != model.MediaWorkflowExecutorRunningHub && record.Executor != model.MediaWorkflowExecutorComfyUI {
		return errors.New("executor must be runninghub or comfyui")
	}
	if record.Executor == model.MediaWorkflowExecutorRunningHub && !regexp.MustCompile(`^\d{6,}$`).MatchString(record.WorkflowID) {
		return errors.New("RunningHub workflow ID must be the number from the /workflow/<id> link")
	}
	if payload.ApiJSON != nil {
		record.ApiJSON = strings.TrimSpace(*payload.ApiJSON)
	}
	if payload.UiJSON != nil {
		record.UiJSON = strings.TrimSpace(*payload.UiJSON)
	}
	workflow, err := mediaworkflow.ParseAPIWorkflow([]byte(record.ApiJSON))
	if err != nil {
		return err
	}
	analysis, err := mediaworkflow.Analyze([]byte(record.ApiJSON), []byte(record.UiJSON))
	if err != nil {
		return err
	}
	analysisJSON, err := common.Marshal(analysis)
	if err != nil {
		return err
	}
	record.Analysis = string(analysisJSON)

	record.MediaKind = strings.TrimSpace(payload.MediaKind)
	if record.MediaKind == "" {
		record.MediaKind = analysis.MediaKind
	}
	switch record.MediaKind {
	case mediaworkflow.KindImage, mediaworkflow.KindVideo, mediaworkflow.KindAudio:
	default:
		return errors.New("media kind must be image, video or audio")
	}

	mapping := mediaworkflow.SuggestMapping(analysis)
	if payload.InputMapping != nil {
		mapping = *payload.InputMapping
	}
	if err := mapping.Validate(workflow); err != nil {
		return err
	}
	outputNodes := payload.OutputNodes
	if len(outputNodes) == 0 {
		outputNodes = mediaworkflow.SuggestOutputNodes(analysis)
	}
	for _, node := range outputNodes {
		if _, ok := workflow[node]; !ok {
			return fmt.Errorf("output node %s is not an active node of the workflow", node)
		}
	}
	mappingJSON, _ := common.Marshal(mapping)
	outputJSON, _ := common.Marshal(outputNodes)
	templateJSON, _ := common.Marshal(payload.PromptTemplate)
	record.InputMapping = string(mappingJSON)
	record.OutputNodes = string(outputJSON)
	record.PromptTemplate = string(templateJSON)
	record.Enabled = payload.Enabled
	return nil
}

func DeleteMediaWorkflow(c *gin.Context) {
	record, ok := mediaWorkflowFromParam(c)
	if !ok {
		return
	}
	if err := record.Delete(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

type mediaWorkflowEnableRequest struct {
	Enabled bool `json:"enabled"`
}

// EnableMediaWorkflow turns a workflow on/off. Turning it on also adds the
// model name to every RunningHub channel so requests can be routed to it.
func EnableMediaWorkflow(c *gin.Context) {
	record, ok := mediaWorkflowFromParam(c)
	if !ok {
		return
	}
	var req mediaWorkflowEnableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	record.Enabled = req.Enabled
	if err := record.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	updated := make([]string, 0)
	if req.Enabled && record.Executor == model.MediaWorkflowExecutorRunningHub {
		channels, err := model.GetChannelsByType(0, 100, true, constant.ChannelTypeRunningHub)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for _, summary := range channels {
			channel, err := model.GetChannelById(summary.Id, true)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			models := channel.GetModels()
			if common.StringsContains(models, record.ModelName) {
				continue
			}
			channel.Models = strings.Join(append(models, record.ModelName), ",")
			if err := channel.Update(); err != nil {
				common.ApiError(c, err)
				return
			}
			updated = append(updated, channel.Name)
		}
		if len(updated) > 0 {
			model.InitChannelCache()
		}
	}
	common.ApiSuccess(c, gin.H{"enabled": record.Enabled, "channels_updated": updated})
}

// runningHubClientForAdmin picks the given RunningHub channel, or the first
// enabled one that has a key.
func runningHubClientForAdmin(channelID int) (*rh.Client, *model.Channel, error) {
	var channel *model.Channel
	if channelID > 0 {
		found, err := model.GetChannelById(channelID, true)
		if err != nil {
			return nil, nil, err
		}
		if found.Type != constant.ChannelTypeRunningHub {
			return nil, nil, errors.New("channel is not a RunningHub channel")
		}
		channel = found
	} else {
		var candidates []*model.Channel
		if err := model.DB.Where("type = ? AND status = ?", constant.ChannelTypeRunningHub, common.ChannelStatusEnabled).
			Order("priority desc, id asc").Find(&candidates).Error; err != nil {
			return nil, nil, err
		}
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate.Key) != "" {
				channel = candidate
				break
			}
		}
	}
	if channel == nil || strings.TrimSpace(channel.Key) == "" {
		return nil, nil, errors.New("no enabled RunningHub channel with an API key yet; add one under Channels (type RunningHub) first")
	}
	key := strings.TrimSpace(strings.Split(strings.TrimSpace(channel.Key), "\n")[0])
	httpClient, err := service.GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		return nil, nil, err
	}
	return rh.New(channel.GetBaseURL(), key, httpClient), channel, nil
}

// GetRunningHubAccount shows the RunningHub balance/queue of a channel key.
// A missing channel or an unreachable account is reported in the data
// (configured=false / error) rather than as a failed request, since the
// page shows it as a status line.
func GetRunningHubAccount(c *gin.Context) {
	channelID, _ := strconv.Atoi(c.Query("channel_id"))
	client, channel, err := runningHubClientForAdmin(channelID)
	if err != nil {
		common.ApiSuccess(c, gin.H{"configured": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	response := gin.H{"configured": true, "channel_id": channel.Id, "channel_name": channel.Name}
	account, err := client.Account(ctx)
	if err != nil {
		response["error"] = err.Error()
	} else {
		response["account"] = account
	}
	common.ApiSuccess(c, response)
}

type mediaWorkflowTestRequest struct {
	ChannelID int      `json:"channel_id"`
	Prompt    string   `json:"prompt"`
	Images    []string `json:"images"`
	Videos    []string `json:"videos"`
	Audios    []string `json:"audios"`
	Duration  int      `json:"duration"`
	Ratio     string   `json:"ratio"`
	// ExtraNodes are appended to the generated nodeInfoList as-is, so an
	// admin can try overrides the mapping cannot express yet (for example
	// unwiring an unused reference slot).
	ExtraNodes []rh.NodeInfo `json:"extra_nodes"`
	// Timeline workflows: segments and audio lock, as in DC-Media metadata.
	Segments  []mediaworkflow.TimelineSegmentSpec `json:"segments"`
	AudioLock *bool                               `json:"audio_lock"`
}

// TestMediaWorkflow submits one real RunningHub task for a saved workflow
// (enabled or not) without going through billing. RunningHub charges the
// account for it as for any run.
func TestMediaWorkflow(c *gin.Context) {
	record, ok := mediaWorkflowFromParam(c)
	if !ok {
		return
	}
	var req mediaWorkflowTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if record.Executor != model.MediaWorkflowExecutorRunningHub {
		common.ApiErrorMsg(c, "test run is only available for RunningHub workflows")
		return
	}
	workflow, err := taskrh.WorkflowFromRecord(record)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	client, channel, err := runningHubClientForAdmin(req.ChannelID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Duration < 0 || req.Duration > 600 {
		common.ApiErrorMsg(c, "duration must be between 0 and 600 seconds")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()
	task, err := workflow.BuildTask(ctx, client, taskrh.Inputs{
		Prompt: req.Prompt, Images: req.Images, Videos: req.Videos, Audios: req.Audios,
		Duration: req.Duration, Ratio: req.Ratio, Segments: req.Segments, AudioLock: req.AudioLock,
	}, taskrh.ReadMediaInput)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	task.NodeInfoList = append(task.NodeInfoList, req.ExtraNodes...)
	created, err := client.CreateTask(ctx, task)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"channel_id":     channel.Id,
		"task_id":        created.TaskID,
		"task_status":    created.TaskStatus,
		"prompt_tips":    created.PromptTips,
		"node_info_list": task.NodeInfoList,
	})
}

// GetMediaWorkflowTestStatus polls a test run started by TestMediaWorkflow.
func GetMediaWorkflowTestStatus(c *gin.Context) {
	record, ok := mediaWorkflowFromParam(c)
	if !ok {
		return
	}
	channelID, _ := strconv.Atoi(c.Query("channel_id"))
	taskID := strings.TrimSpace(c.Query("task_id"))
	if !regexp.MustCompile(`^\d+$`).MatchString(taskID) {
		common.ApiErrorMsg(c, "invalid task_id")
		return
	}
	client, _, err := runningHubClientForAdmin(channelID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	status, err := client.TaskStatus(ctx, taskID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response := gin.H{"status": status}
	if status == rh.StatusSuccess || status == rh.StatusFailed {
		outputs, err := client.TaskOutputs(ctx, taskID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		response["outputs"] = outputs.Outputs
		if outputs.State == rh.StatusFailed {
			response["error"] = taskrh.DescribeFailure(outputs)
		}
		var outputNodes []string
		_ = common.UnmarshalJsonStr(record.OutputNodes, &outputNodes)
		if picked := taskrh.PickOutputs(outputs.Outputs, record.MediaKind, outputNodes); len(picked) > 0 {
			response["result"] = picked[0]
		}
	}
	common.ApiSuccess(c, response)
}
