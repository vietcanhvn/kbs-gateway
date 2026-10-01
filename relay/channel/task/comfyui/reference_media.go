package comfyui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const (
	miniMaxH3ReferenceNode = "MiniMaxH3ReferenceToVideo"
	miniMaxH3ImageLimit    = 9
	miniMaxH3VideoLimit    = 3
	miniMaxH3AudioLimit    = 3

	// Qwen-Image-Edit (2509 / 2511): the positive "Edit Plus" encoder takes up
	// to three images on image1..image3.
	qwenEditPlusNode       = "TextEncodeQwenImageEditPlus"
	qwenEditPlusImageLimit = 3
	// Qwen Image 2.1 reference workflow: one "References Manager" node lists
	// up to ten input files in its references_json widget and fans them out.
	qwenReferencePackNode       = "QwenImageReferencePack"
	qwenReferencePackImageLimit = 10
	// Qwen Image 2.1 Image Edit (ComfyUI's own template): images go straight
	// into the encoder on images.image_1..images.image_10.
	qwen21EncodeNode       = "TextEncodeQwenImage21"
	qwen21EncodeImageLimit = 10
)

// Which node a reference workflow feeds, and how.
const (
	referenceKindMiniMaxH3     = "minimax_h3"
	referenceKindQwenEditPlus  = "qwen_edit_plus"
	referenceKindQwenReference = "qwen_reference_pack"
	referenceKindQwen21Encode  = "qwen21_encode"
)

// Image input name for slot n (1-based) on the Qwen encoders that take images directly.
var qwenImageInputFormat = map[string]string{
	referenceKindQwenEditPlus: "image%d",
	referenceKindQwen21Encode: "images.image_%d",
}

var referenceInputPrefixes = []string{
	"ref_images.ref_image_",
	"ref_videos.ref_video_",
	"ref_video_audios.ref_video_audio_",
	"ref_audios.ref_audio_",
}

type referenceWorkflowSpec struct {
	kind         string
	targetNodeID string
	imageLimit   int
	videoLimit   int
	audioLimit   int
}

type referenceValidationError struct {
	field   string
	message string
	count   int
	limit   int
}

func (e *referenceValidationError) Error() string            { return e.message }
func (e *referenceValidationError) TaskErrorCode() string    { return "invalid_request" }
func (e *referenceValidationError) TaskErrorStatusCode() int { return http.StatusBadRequest }
func (e *referenceValidationError) TaskErrorLocal() bool     { return true }
func (e *referenceValidationError) TaskErrorData() any {
	data := map[string]any{"field": e.field}
	if e.limit > 0 {
		data["count"] = e.count
		data["limit"] = e.limit
	}
	return data
}

func (a *TaskAdaptor) applyReferenceMediaToWorkflow(
	c *gin.Context,
	workflow map[string]any,
	req common.TaskSubmitReq,
	metadata comfyMetadata,
	info *common.RelayInfo,
) (bool, error) {
	spec, ok := referenceWorkflowSpecFor(workflow)
	if !ok {
		return false, nil
	}
	if spec.kind != referenceKindMiniMaxH3 {
		return true, a.applyQwenReferenceImages(c, workflow, spec, req, metadata, info)
	}

	images := nonEmptyReferences(append(req.AdditionalReferenceImages(), metadata.ReferenceImages...))
	videos := nonEmptyReferences(metadata.ReferenceVideos)
	audios := nonEmptyReferences(metadata.ReferenceAudios)
	if err := validateReferenceCounts(spec, images, videos, audios); err != nil {
		return true, err
	}
	targetInputs, err := workflowNodeInputs(workflow, spec.targetNodeID)
	if err != nil {
		return true, err
	}
	removeExistingReferenceInputs(workflow, spec.targetNodeID, targetInputs)
	removeUnusedReferenceLoaders(workflow, spec.targetNodeID)
	allocator := newWorkflowNodeIDAllocator(workflow)

	for index, source := range images {
		name, err := a.uploadReferenceInput(c, info, source, "image")
		if err != nil {
			return true, fmt.Errorf("upload reference image %d: %w", index+1, err)
		}
		loadID := allocator.next()
		workflow[loadID] = workflowNode("LoadImage", map[string]any{"image": name})
		targetInputs[fmt.Sprintf("ref_images.ref_image_%d", index)] = []any{loadID, 0}
	}

	for index, source := range videos {
		name, err := a.uploadReferenceInput(c, info, source, "video")
		if err != nil {
			return true, fmt.Errorf("upload reference video %d: %w", index+1, err)
		}
		loadID := allocator.next()
		componentsID := allocator.next()
		workflow[loadID] = workflowNode("LoadVideo", map[string]any{"file": name})
		workflow[componentsID] = workflowNode("GetVideoComponents", map[string]any{
			"video": []any{loadID, 0},
		})
		targetInputs[fmt.Sprintf("ref_videos.ref_video_%d", index)] = []any{componentsID, 0}
		targetInputs[fmt.Sprintf("ref_video_audios.ref_video_audio_%d", index)] = []any{componentsID, 1}
	}

	for index, source := range audios {
		name, err := a.uploadReferenceInput(c, info, source, "audio")
		if err != nil {
			return true, fmt.Errorf("upload reference audio %d: %w", index+1, err)
		}
		loadID := allocator.next()
		workflow[loadID] = workflowNode("LoadAudio", map[string]any{"audio": name})
		targetInputs[fmt.Sprintf("ref_audios.ref_audio_%d", index)] = []any{loadID, 0}
	}
	return true, nil
}

func referenceWorkflowSpecFor(workflow map[string]any) (referenceWorkflowSpec, bool) {
	ids := make([]string, 0, len(workflow))
	for id := range workflow {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	classOf := func(id string) string {
		node, ok := workflow[id].(map[string]any)
		if !ok {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(node["class_type"]))
	}
	for _, id := range ids {
		if strings.EqualFold(classOf(id), miniMaxH3ReferenceNode) {
			return referenceWorkflowSpec{
				kind:         referenceKindMiniMaxH3,
				targetNodeID: id,
				imageLimit:   miniMaxH3ImageLimit,
				videoLimit:   miniMaxH3VideoLimit,
				audioLimit:   miniMaxH3AudioLimit,
			}, true
		}
	}
	for _, id := range ids {
		if strings.EqualFold(classOf(id), qwenReferencePackNode) {
			return referenceWorkflowSpec{
				kind:         referenceKindQwenReference,
				targetNodeID: id,
				imageLimit:   qwenReferencePackImageLimit,
			}, true
		}
	}
	// Encoders that take images directly: only the one already wired to an
	// image counts (the negative encoder, or a text-only workflow that happens
	// to use the same node, has none).
	for _, candidate := range []struct {
		kind, class string
		limit       int
	}{
		{referenceKindQwen21Encode, qwen21EncodeNode, qwen21EncodeImageLimit},
		{referenceKindQwenEditPlus, qwenEditPlusNode, qwenEditPlusImageLimit},
	} {
		firstSlot := fmt.Sprintf(qwenImageInputFormat[candidate.kind], 1)
		for _, id := range ids {
			if !strings.EqualFold(classOf(id), candidate.class) {
				continue
			}
			inputs, _ := workflow[id].(map[string]any)["inputs"].(map[string]any)
			if workflowConnectionNodeID(inputs[firstSlot]) != "" {
				return referenceWorkflowSpec{kind: candidate.kind, targetNodeID: id, imageLimit: candidate.limit}, true
			}
		}
	}
	return referenceWorkflowSpec{}, false
}

// qwenReferenceImages: every image of the request, top-level first. Qwen
// workflows have no "first frame": the main image is simply image 1.
func qwenReferenceImages(req common.TaskSubmitReq, metadata comfyMetadata) []string {
	all := append([]string{req.Image}, req.AdditionalReferenceImages()...)
	return nonEmptyReferences(append(all, metadata.ReferenceImages...))
}

// applyQwenReferenceImages uploads the request's images and wires them into a
// Qwen edit / reference workflow, replacing the sample images it shipped with.
func (a *TaskAdaptor) applyQwenReferenceImages(
	c *gin.Context,
	workflow map[string]any,
	spec referenceWorkflowSpec,
	req common.TaskSubmitReq,
	metadata comfyMetadata,
	info *common.RelayInfo,
) error {
	images := qwenReferenceImages(req, metadata)
	if err := validateReferenceCounts(spec, images, nonEmptyReferences(metadata.ReferenceVideos), nonEmptyReferences(metadata.ReferenceAudios)); err != nil {
		return err
	}
	if len(images) == 0 {
		return &referenceValidationError{field: "image", message: "this workflow needs at least one input image"}
	}
	inputs, err := workflowNodeInputs(workflow, spec.targetNodeID)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(images))
	for index, source := range images {
		name, err := a.uploadReferenceInput(c, info, source, "image")
		if err != nil {
			return fmt.Errorf("upload reference image %d: %w", index+1, err)
		}
		names = append(names, name)
	}

	switch spec.kind {
	case referenceKindQwenReference:
		refs := make([]map[string]string, 0, len(names))
		for _, name := range names {
			refs = append(refs, map[string]string{"kind": "image", "file": name})
		}
		encoded, err := json.Marshal(map[string]any{"references": refs})
		if err != nil {
			return err
		}
		inputs["references_json"] = string(encoded)
	case referenceKindQwenEditPlus, referenceKindQwen21Encode:
		format := qwenImageInputFormat[spec.kind]
		oldRoots := make([]string, 0)
		for slot := 1; slot <= spec.imageLimit; slot++ {
			key := fmt.Sprintf(format, slot)
			if nodeID := workflowConnectionNodeID(inputs[key]); nodeID != "" {
				oldRoots = append(oldRoots, nodeID)
			}
			delete(inputs, key)
		}
		for _, nodeID := range oldRoots {
			removeOrphanedReferenceChain(workflow, spec.targetNodeID, nodeID)
		}
		removeUnusedReferenceLoaders(workflow, spec.targetNodeID)
		allocator := newWorkflowNodeIDAllocator(workflow)
		for index, name := range names {
			loadID := allocator.next()
			workflow[loadID] = workflowNode("LoadImage", map[string]any{"image": name})
			inputs[fmt.Sprintf(format, index+1)] = []any{loadID, 0}
		}
	}
	return nil
}

func validateReferenceCounts(spec referenceWorkflowSpec, images, videos, audios []string) error {
	for _, item := range []struct {
		name  string
		count int
		limit int
	}{
		{"reference_images", len(images), spec.imageLimit},
		{"reference_videos", len(videos), spec.videoLimit},
		{"reference_audios", len(audios), spec.audioLimit},
	} {
		if item.count > item.limit {
			return &referenceValidationError{
				field:   item.name,
				message: fmt.Sprintf("%s supports at most %d items, got %d", item.name, item.limit, item.count),
				count:   item.count,
				limit:   item.limit,
			}
		}
	}
	if len(audios) > 0 && len(images) == 0 && len(videos) == 0 {
		return &referenceValidationError{
			field:   "reference_audios",
			message: "reference_audios cannot be used alone; provide at least one reference image or video",
		}
	}
	return nil
}

func removeExistingReferenceInputs(workflow map[string]any, targetNodeID string, inputs map[string]any) {
	oldRoots := make([]string, 0)
	for key, value := range inputs {
		if !hasReferenceInputPrefix(key) {
			continue
		}
		if nodeID := workflowConnectionNodeID(value); nodeID != "" {
			oldRoots = append(oldRoots, nodeID)
		}
		delete(inputs, key)
	}
	for _, nodeID := range oldRoots {
		removeOrphanedReferenceChain(workflow, targetNodeID, nodeID)
	}
}

func removeUnusedReferenceLoaders(workflow map[string]any, targetNodeID string) {
	for {
		removed := false
		for nodeID, raw := range workflow {
			if nodeID == targetNodeID || workflowNodeIsReferenced(workflow, "", nodeID) {
				continue
			}
			node, ok := raw.(map[string]any)
			if !ok || !isReferenceLoaderClass(fmt.Sprint(node["class_type"])) {
				continue
			}
			removeOrphanedReferenceChain(workflow, "", nodeID)
			removed = true
		}
		if !removed {
			return
		}
	}
}

func removeOrphanedReferenceChain(workflow map[string]any, targetNodeID, nodeID string) {
	if nodeID == "" || workflowNodeIsReferenced(workflow, targetNodeID, nodeID) {
		return
	}
	node, ok := workflow[nodeID].(map[string]any)
	if !ok || !isReferenceLoaderClass(fmt.Sprint(node["class_type"])) {
		return
	}
	dependencies := workflowInputConnections(node)
	delete(workflow, nodeID)
	for _, dependency := range dependencies {
		removeOrphanedReferenceChain(workflow, targetNodeID, dependency)
	}
}

func workflowNodeIsReferenced(workflow map[string]any, excludedNodeID, wantedNodeID string) bool {
	for nodeID, raw := range workflow {
		if nodeID == excludedNodeID {
			continue
		}
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, dependency := range workflowInputConnections(node) {
			if dependency == wantedNodeID {
				return true
			}
		}
	}
	return false
}

func workflowInputConnections(node map[string]any) []string {
	inputs, _ := node["inputs"].(map[string]any)
	connections := make([]string, 0)
	for _, value := range inputs {
		if nodeID := workflowConnectionNodeID(value); nodeID != "" {
			connections = append(connections, nodeID)
		}
	}
	return connections
}

func workflowConnectionNodeID(value any) string {
	connection, ok := value.([]any)
	if !ok || len(connection) < 2 {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(connection[0]))
}

func hasReferenceInputPrefix(key string) bool {
	for _, prefix := range referenceInputPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func isReferenceLoaderClass(classType string) bool {
	switch strings.ToLower(strings.TrimSpace(classType)) {
	case "loadimage", "loadvideo", "getvideocomponents", "loadaudio":
		return true
	default:
		return false
	}
}

func workflowNodeInputs(workflow map[string]any, nodeID string) (map[string]any, error) {
	node, ok := workflow[nodeID].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("comfyui workflow node %q not found", nodeID)
	}
	inputs, ok := node["inputs"].(map[string]any)
	if !ok {
		inputs = map[string]any{}
		node["inputs"] = inputs
	}
	return inputs, nil
}

func workflowNode(classType string, inputs map[string]any) map[string]any {
	return map[string]any{"class_type": classType, "inputs": inputs}
}

func nonEmptyReferences(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

type workflowNodeIDAllocator struct {
	workflow map[string]any
	nextID   int
}

func newWorkflowNodeIDAllocator(workflow map[string]any) *workflowNodeIDAllocator {
	maxID := 0
	for id := range workflow {
		if value, err := strconv.Atoi(id); err == nil && value > maxID {
			maxID = value
		}
	}
	return &workflowNodeIDAllocator{workflow: workflow, nextID: maxID + 1}
}

func (a *workflowNodeIDAllocator) next() string {
	for {
		id := strconv.Itoa(a.nextID)
		a.nextID++
		if _, exists := a.workflow[id]; !exists {
			return id
		}
	}
}

func (a *TaskAdaptor) uploadReferenceInput(c *gin.Context, info *common.RelayInfo, input, mediaType string) (string, error) {
	if a.uploadInput != nil {
		return a.uploadInput(c, info, input, mediaType)
	}
	return a.uploadComfyUIInput(c, info, input, mediaType)
}
