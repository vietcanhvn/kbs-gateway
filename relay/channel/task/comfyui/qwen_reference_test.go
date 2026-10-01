package comfyui

import (
	"encoding/json"
	"strconv"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func qwenUploader(uploads *[]string) *TaskAdaptor {
	return &TaskAdaptor{
		uploadInput: func(_ *gin.Context, _ *relaycommon.RelayInfo, source, mediaType string) (string, error) {
			*uploads = append(*uploads, source)
			return "up-" + strconv.Itoa(len(*uploads)) + ".png", nil
		},
	}
}

// ComfyUI's "Qwen Image 2.1: Image Edit" template, trimmed to the parts that matter.
func qwen21EditWorkflow() map[string]any {
	return map[string]any{
		"470": workflowNode("LoadImage", map[string]any{"image": "portrait_model_denim.png"}),
		"475": workflowNode("LoadImage", map[string]any{"image": "clothing_light_blue_denim_shirt.png"}),
		"459:474": workflowNode("TextEncodeQwenImage21", map[string]any{
			"prompt": "x", "images.image_1": []any{"470", 0}, "images.image_2": []any{"475", 0},
		}),
	}
}

func TestQwen21EditWiresEveryImageAndDropsTheSamples(t *testing.T) {
	var uploads []string
	workflow := qwen21EditWorkflow()
	req := relaycommon.TaskSubmitReq{Image: "data:a", Images: []string{"data:a", "data:b", "data:c"}}

	handled, err := qwenUploader(&uploads).applyReferenceMediaToWorkflow(nil, workflow, req, comfyMetadata{}, &relaycommon.RelayInfo{})
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, []string{"data:a", "data:b", "data:c"}, uploads, "top-level image is image 1")

	inputs := workflow["459:474"].(map[string]any)["inputs"].(map[string]any)
	for slot := 1; slot <= 3; slot++ {
		assertWorkflowConnectionClass(t, workflow, inputs["images.image_"+strconv.Itoa(slot)], "LoadImage", 0)
	}
	assertWorkflowDoesNotContainInputValue(t, workflow, "portrait_model_denim.png")
	assertWorkflowDoesNotContainInputValue(t, workflow, "clothing_light_blue_denim_shirt.png")
}

func TestQwenEditPlusTakesOneImageWithoutLeavingTheSecondSample(t *testing.T) {
	var uploads []string
	workflow := map[string]any{
		"166": workflowNode("LoadImage", map[string]any{"image": "sample1.jpeg"}),
		"177": workflowNode("LoadImage", map[string]any{"image": "sample2.png"}),
		"179": workflowNode("TextEncodeQwenImageEditPlus", map[string]any{"prompt": "p", "image1": []any{"166", 0}, "image2": []any{"177", 0}}),
		"182": workflowNode("TextEncodeQwenImageEditPlus", map[string]any{"prompt": ""}),
	}
	_, err := qwenUploader(&uploads).applyReferenceMediaToWorkflow(nil, workflow, relaycommon.TaskSubmitReq{Image: "data:src"}, comfyMetadata{}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	inputs := workflow["179"].(map[string]any)["inputs"].(map[string]any)
	assertWorkflowConnectionClass(t, workflow, inputs["image1"], "LoadImage", 0)
	assert.NotContains(t, inputs, "image2")
	assertWorkflowDoesNotContainInputValue(t, workflow, "sample1.jpeg")
	assertWorkflowDoesNotContainInputValue(t, workflow, "sample2.png")
	assert.NotContains(t, workflow["182"].(map[string]any)["inputs"].(map[string]any), "image1", "negative encoder untouched")
}

func TestQwenReferencePackListsUploadedFiles(t *testing.T) {
	var uploads []string
	workflow := map[string]any{
		"37": workflowNode("QwenImageReferencePack", map[string]any{"references_json": `{"references":[{"kind":"image","file":"output.png"}]}`}),
	}
	meta := comfyMetadata{ReferenceImages: []string{"data:b"}}
	_, err := qwenUploader(&uploads).applyReferenceMediaToWorkflow(nil, workflow, relaycommon.TaskSubmitReq{Image: "data:a"}, meta, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	var got struct {
		References []map[string]string `json:"references"`
	}
	require.NoError(t, json.Unmarshal([]byte(workflow["37"].(map[string]any)["inputs"].(map[string]any)["references_json"].(string)), &got))
	assert.Equal(t, []map[string]string{{"kind": "image", "file": "up-1.png"}, {"kind": "image", "file": "up-2.png"}}, got.References)
}

func TestQwenRejectsTooManyImages(t *testing.T) {
	var uploads []string
	images := make([]string, 4)
	for i := range images {
		images[i] = "data:" + strconv.Itoa(i)
	}
	workflow := map[string]any{
		"166": workflowNode("LoadImage", map[string]any{"image": "s.png"}),
		"179": workflowNode("TextEncodeQwenImageEditPlus", map[string]any{"image1": []any{"166", 0}}),
	}
	_, err := qwenUploader(&uploads).applyReferenceMediaToWorkflow(nil, workflow, relaycommon.TaskSubmitReq{Images: images}, comfyMetadata{}, &relaycommon.RelayInfo{})
	require.Error(t, err)
	assert.Empty(t, uploads, "nothing uploaded when the request is over the limit")
}

func TestTextOnlyQwenWorkflowIsNotAReferenceWorkflow(t *testing.T) {
	workflow := map[string]any{
		"452": workflowNode("TextEncodeQwenImage21", map[string]any{"prompt": "p"}),
	}
	_, ok := referenceWorkflowSpecFor(workflow)
	assert.False(t, ok)
}
