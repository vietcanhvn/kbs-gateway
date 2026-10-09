package runninghub

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/mediaworkflow"
	rh "github.com/QuantumNous/new-api/pkg/mediaworkflow/runninghub"
	"github.com/QuantumNous/new-api/service"
)

// Workflow is a registry entry decoded for execution.
type Workflow struct {
	ModelName   string
	WorkflowID  string
	MediaKind   string
	Mapping     mediaworkflow.InputMapping
	Template    mediaworkflow.PromptTemplate
	OutputNodes []string
	// DefaultSeconds is the value already set in the workflow's duration
	// input; it is what RunningHub renders when a request sends no duration.
	DefaultSeconds float64
	// nodes is the stored API JSON, used to find what an unused media
	// loader feeds so it can be unwired.
	nodes map[string]map[string]any
}

// LoadWorkflow returns the enabled RunningHub workflow registered for a model.
func LoadWorkflow(modelName string) (*Workflow, error) {
	record, err := model.GetEnabledMediaWorkflow(modelName, model.MediaWorkflowExecutorRunningHub)
	if err != nil {
		return nil, err
	}
	return WorkflowFromRecord(record)
}

// WorkflowFromRecord decodes the JSON columns of a registry row.
func WorkflowFromRecord(record *model.MediaWorkflow) (*Workflow, error) {
	workflow := &Workflow{
		ModelName:  record.ModelName,
		WorkflowID: strings.TrimSpace(record.WorkflowID),
		MediaKind:  record.MediaKind,
	}
	if workflow.WorkflowID == "" {
		return nil, fmt.Errorf("workflow %q has no RunningHub workflow ID", record.ModelName)
	}
	if text := strings.TrimSpace(record.InputMapping); text != "" {
		if err := common.UnmarshalJsonStr(text, &workflow.Mapping); err != nil {
			return nil, fmt.Errorf("workflow %q input mapping: %w", record.ModelName, err)
		}
	}
	if text := strings.TrimSpace(record.PromptTemplate); text != "" {
		if err := common.UnmarshalJsonStr(text, &workflow.Template); err != nil {
			return nil, fmt.Errorf("workflow %q prompt template: %w", record.ModelName, err)
		}
	}
	if text := strings.TrimSpace(record.OutputNodes); text != "" {
		if err := common.UnmarshalJsonStr(text, &workflow.OutputNodes); err != nil {
			return nil, fmt.Errorf("workflow %q output nodes: %w", record.ModelName, err)
		}
	}
	if nodes, err := mediaworkflow.ParseAPIWorkflow([]byte(record.ApiJSON)); err == nil {
		workflow.nodes = nodes
	}
	workflow.DefaultSeconds = defaultDuration(workflow.nodes, workflow.Mapping)
	return workflow, nil
}

// defaultDuration reads the current value of the mapped duration input from
// the stored API JSON nodes (0 when there is none or it is not a number).
func defaultDuration(nodes map[string]map[string]any, mapping mediaworkflow.InputMapping) float64 {
	for _, binding := range mapping.Inputs {
		if binding.Role != mediaworkflow.RoleDuration {
			continue
		}
		inputs, _ := nodes[binding.NodeID]["inputs"].(map[string]any)
		switch value := inputs[binding.Field].(type) {
		case float64:
			return value
		case string:
			parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
			return parsed
		}
		return 0
	}
	return 0
}

func (w *Workflow) timelineBinding() (mediaworkflow.InputBinding, bool) {
	for _, binding := range w.Mapping.Inputs {
		if binding.Role == mediaworkflow.RoleTimeline {
			return binding, true
		}
	}
	return mediaworkflow.InputBinding{}, false
}

// PlanTimeline resolves the segments of a timeline request.
func (w *Workflow) PlanTimeline(in Inputs) ([]mediaworkflow.TimelineSegment, error) {
	return mediaworkflow.PlanTimeline(in.Prompt, in.Segments, float64(in.Duration), len(in.Images), len(in.Videos), len(in.Audios))
}

// TimelineSeconds is the billed length of a timeline request (sum of its
// segments at the template's frame rate); ok is false for other workflows.
func (w *Workflow) TimelineSeconds(in Inputs) (float64, bool, error) {
	timeline, ok := w.timelineBinding()
	if !ok {
		return 0, false, nil
	}
	segments, err := w.PlanTimeline(in)
	if err != nil {
		return 0, true, err
	}
	template, _ := w.nodes[timeline.NodeID]["inputs"].(map[string]any)[timeline.Field].(string)
	return mediaworkflow.TimelineSeconds(segments, mediaworkflow.TimelineFrameRate(template)), true, nil
}

// Inputs is a DC-Media request reduced to workflow inputs. Media entries are
// data URLs or http(s) URLs; they are uploaded to RunningHub before the run.
type Inputs struct {
	Prompt         string
	NegativePrompt string
	Images         []string
	Videos         []string
	Audios         []string
	LastFrame      string
	Duration       int
	Width          int
	Height         int
	Ratio          string
	Seed           int64
	// Timeline workflows: segments (DC-Media metadata.segments) and whether the
	// first audio is the soundtrack the video follows (lip-sync).
	Segments  []mediaworkflow.TimelineSegmentSpec
	AudioLock *bool
}

// MediaReader loads one input (data URL or URL) as a file.
type MediaReader func(ctx context.Context, input, kind string) (filename string, data []byte, err error)

// BuildTask uploads the request media and resolves the node overrides.
func (w *Workflow) BuildTask(ctx context.Context, client *rh.Client, in Inputs, read MediaReader) (rh.CreateTaskRequest, error) {
	capacity := w.Mapping.MediaCapacity()
	for kind, items := range map[string][]string{mediaworkflow.RoleImage: in.Images, mediaworkflow.RoleVideo: in.Videos, mediaworkflow.RoleAudio: in.Audios} {
		if len(items) > capacity[kind] {
			return rh.CreateTaskRequest{}, &mediaworkflow.RequestError{Message: fmt.Sprintf("model %s accepts at most %d %s input(s), got %d", w.ModelName, capacity[kind], kind, len(items))}
		}
	}
	upload := func(items []string, kind string) ([]string, error) {
		names := make([]string, 0, len(items))
		for _, item := range items {
			filename, data, err := read(ctx, item, kind)
			if err != nil {
				return nil, &mediaworkflow.RequestError{Message: fmt.Sprintf("read %s input: %v", kind, err)}
			}
			name, err := client.Upload(ctx, filename, data)
			if err != nil {
				return nil, err
			}
			names = append(names, name)
		}
		return names, nil
	}
	// Always render the duration that is billed: a request without one used
	// to leave the workflow's own value in place, which can differ from what
	// the gateway charged (e.g. "0 = whole audio" in lip-sync workflows).
	duration := in.Duration
	if w.Mapping.HasRole(mediaworkflow.RoleDuration) {
		duration = BilledSeconds(w.Mapping, in.Duration, w.DefaultSeconds)
	}
	request := mediaworkflow.MediaRequest{
		Prompt:         w.Template.Render(in.Prompt),
		NegativePrompt: in.NegativePrompt,
		Duration:       duration,
		Width:          in.Width,
		Height:         in.Height,
		Ratio:          in.Ratio,
		Seed:           in.Seed,
	}
	var err error
	if request.Images, err = upload(in.Images, mediaworkflow.KindImage); err != nil {
		return rh.CreateTaskRequest{}, err
	}
	if request.Videos, err = upload(in.Videos, mediaworkflow.KindVideo); err != nil {
		return rh.CreateTaskRequest{}, err
	}
	if request.Audios, err = upload(in.Audios, mediaworkflow.KindAudio); err != nil {
		return rh.CreateTaskRequest{}, err
	}
	if strings.TrimSpace(in.LastFrame) != "" {
		names, err := upload([]string{in.LastFrame}, mediaworkflow.KindImage)
		if err != nil {
			return rh.CreateTaskRequest{}, err
		}
		request.LastFrame = names[0]
	}
	overrides, err := mediaworkflow.BuildNodeOverrides(w.Mapping, request)
	if err != nil {
		return rh.CreateTaskRequest{}, err
	}
	overrides = append(overrides, mediaworkflow.UnwireUnusedMedia(w.Mapping, w.nodes, request)...)
	if timeline, ok := w.timelineBinding(); ok {
		template, _ := w.nodes[timeline.NodeID]["inputs"].(map[string]any)[timeline.Field].(string)
		segments, err := w.PlanTimeline(in)
		if err != nil {
			return rh.CreateTaskRequest{}, err
		}
		// Default: one attached audio is the soundtrack, unless parts chose
		// their own audios (voice / sound references per part).
		lock := len(request.Audios) > 0
		for _, segment := range segments {
			lock = lock && segment.Audios == nil
		}
		if in.AudioLock != nil {
			lock = *in.AudioLock
		}
		trackData, err := mediaworkflow.BuildTrackData(template, segments, mediaworkflow.TimelineMedia{
			Images: request.Images, Videos: request.Videos, Audios: request.Audios, AudioLock: lock,
		}, w.Template.Render)
		if err != nil {
			return rh.CreateTaskRequest{}, err
		}
		overrides = append(overrides, mediaworkflow.NodeOverride{NodeID: timeline.NodeID, FieldName: timeline.Field, FieldValue: trackData})
	}
	nodeInfoList := make([]rh.NodeInfo, 0, len(overrides))
	for _, override := range overrides {
		nodeInfoList = append(nodeInfoList, rh.NodeInfo{NodeID: override.NodeID, FieldName: override.FieldName, FieldValue: override.FieldValue})
	}
	return rh.CreateTaskRequest{WorkflowID: w.WorkflowID, NodeInfoList: nodeInfoList, InstanceType: w.Mapping.InstanceType}, nil
}

// UpstreamTaskID packs the RunningHub task ID with the output preference so
// the polling loop, which only sees the upstream ID, can pick the right file.
// Format: "<taskId>|<kind>|<node,node>".
func UpstreamTaskID(taskID, kind string, outputNodes []string) string {
	return taskID + "|" + kind + "|" + strings.Join(outputNodes, ",")
}

// ParseUpstreamTaskID reverses UpstreamTaskID. A bare ID is accepted.
func ParseUpstreamTaskID(value string) (taskID, kind string, outputNodes []string) {
	parts := strings.SplitN(strings.TrimSpace(value), "|", 3)
	taskID = parts[0]
	if len(parts) > 1 {
		kind = parts[1]
	}
	if len(parts) > 2 && parts[2] != "" {
		outputNodes = strings.Split(parts[2], ",")
	}
	return taskID, kind, outputNodes
}

// OutputKind classifies a RunningHub fileType.
func OutputKind(fileType string) string {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(fileType), ".")) {
	case "mp4", "webm", "mov", "avi", "mkv", "gif":
		return mediaworkflow.KindVideo
	case "png", "jpg", "jpeg", "webp", "bmp":
		return mediaworkflow.KindImage
	case "wav", "mp3", "flac", "ogg", "m4a":
		return mediaworkflow.KindAudio
	}
	return ""
}

// PickOutputs returns the outputs to deliver: files of the wanted kind from
// the configured output nodes (in node order), else any file of that kind,
// else any media file.
func PickOutputs(outputs []rh.Output, kind string, outputNodes []string) []rh.Output {
	matches := func(output rh.Output) bool { return kind == "" || OutputKind(output.FileType) == kind }
	picked := make([]rh.Output, 0)
	for _, node := range outputNodes {
		for _, output := range outputs {
			if output.NodeID == strings.TrimSpace(node) && matches(output) {
				picked = append(picked, output)
			}
		}
	}
	if len(picked) > 0 {
		return picked
	}
	for _, output := range outputs {
		if matches(output) {
			picked = append(picked, output)
		}
	}
	if len(picked) > 0 {
		return picked
	}
	for _, output := range outputs {
		if OutputKind(output.FileType) != "" {
			picked = append(picked, output)
		}
	}
	return picked
}

// DescribeFailure turns RunningHub's failedReason into a short message.
func DescribeFailure(result *rh.OutputsResult) string {
	if result == nil || result.Failure == nil {
		if result != nil && strings.TrimSpace(result.Message) != "" {
			return "RunningHub task failed: " + strings.TrimSpace(result.Message)
		}
		return "RunningHub task failed"
	}
	failure := result.Failure
	message := strings.TrimSpace(failure.ExceptionMessage)
	if len(message) > 500 {
		message = message[:500] + "..."
	}
	where := ""
	if node := strings.TrimSpace(failure.NodeID + " " + failure.NodeName); node != "" {
		where = " at node " + node
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "not in list") || strings.Contains(lower, "not in [") || strings.Contains(lower, "no such file") ||
		strings.Contains(lower, "filenotfound") || strings.Contains(lower, "could not find") {
		return fmt.Sprintf("RunningHub task failed%s: a model or input file is missing in the RunningHub account (%s)", where, message)
	}
	return fmt.Sprintf("RunningHub task failed%s: %s", where, message)
}

// ReadMediaInput loads a data URL or downloads an http(s) URL (SSRF-checked),
// bounded by RunningHub's 30 MB upload limit.
func ReadMediaInput(_ context.Context, input, kind string) (string, []byte, error) {
	input = strings.TrimSpace(input)
	maxBytes := rh.MaxUploadBytes
	if strings.HasPrefix(input, "data:") {
		header, payload, ok := strings.Cut(input, ",")
		if !ok || !strings.Contains(header, ";base64") {
			return "", nil, fmt.Errorf("invalid data URL")
		}
		if int64(base64.StdEncoding.DecodedLen(len(payload))) > maxBytes+2 {
			return "", nil, fmt.Errorf("%s is larger than 30 MB", kind)
		}
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return "", nil, fmt.Errorf("invalid data URL: %w", err)
		}
		mimeType := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
		return "ksb-input" + extensionFor(mimeType, kind), data, nil
	}
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		return "", nil, fmt.Errorf("unsupported %s input: only data URLs and http(s) URLs are accepted", kind)
	}
	resp, err := service.DoDownloadRequest(input, "runninghub "+kind+" input")
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return "", nil, fmt.Errorf("download %s failed: HTTP %d", kind, resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return "", nil, fmt.Errorf("%s is larger than 30 MB", kind)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", nil, err
	}
	if int64(len(data)) > maxBytes {
		return "", nil, fmt.Errorf("%s is larger than 30 MB", kind)
	}
	filename := "ksb-input" + extensionFor(resp.Header.Get("Content-Type"), kind)
	if parsed, err := url.Parse(input); err == nil {
		if extension := strings.ToLower(path.Ext(parsed.Path)); extensionAllowed(extension, kind) {
			filename = "ksb-input" + extension
		}
	}
	return filename, data, nil
}

func extensionFor(mimeType, kind string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0])) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/flac":
		return ".flac"
	}
	switch kind {
	case mediaworkflow.KindVideo:
		return ".mp4"
	case mediaworkflow.KindAudio:
		return ".mp3"
	}
	return ".png"
}

func extensionAllowed(extension, kind string) bool {
	switch kind {
	case mediaworkflow.KindImage:
		return extension == ".png" || extension == ".jpg" || extension == ".jpeg" || extension == ".webp"
	case mediaworkflow.KindVideo:
		return extension == ".mp4" || extension == ".mov" || extension == ".avi" || extension == ".mkv"
	case mediaworkflow.KindAudio:
		return extension == ".mp3" || extension == ".wav" || extension == ".flac"
	}
	return false
}
