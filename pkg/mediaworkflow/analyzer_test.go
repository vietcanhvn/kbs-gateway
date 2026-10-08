package mediaworkflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return data
}

func candidatesByRole(inputs []InputCandidate, active bool) map[string][]string {
	result := map[string][]string{}
	for _, input := range inputs {
		if input.Active == active {
			result[input.Role] = append(result[input.Role], input.NodeID+"."+input.Field)
		}
	}
	return result
}

func TestAnalyzeReferenceVideoWorkflow(t *testing.T) {
	analysis, err := Analyze(readFixture(t, "ref_video_api.json"), readFixture(t, "ref_video_ui.json"))
	require.NoError(t, err)

	assert.Equal(t, 15, analysis.ActiveNodes)
	assert.Equal(t, 6, analysis.DisabledNodes, "bypassed and muted nodes; editor-only helpers are not counted")
	assert.Equal(t, KindVideo, analysis.MediaKind)

	assert.Equal(t, map[string][]string{
		RolePrompt:         {"21.text"},
		RoleNegativePrompt: {"22.text"},
		RoleImage:          {"20.image"},
		RoleDuration:       {"23.value"},
		RoleAspectRatio:    {"24.aspect_ratio"},
		RoleSeed:           {"25.noise_seed"},
	}, candidatesByRole(analysis.Inputs, true))
	assert.Equal(t, map[string][]string{
		RoleImage: {"60.image", "61.image"},
		RoleVideo: {"62.video"},
		RoleAudio: {"63.audio"},
	}, candidatesByRole(analysis.Inputs, false), "inputs on bypassed nodes are reported as available but disabled")

	activeModels := []string{}
	disabledModels := []string{}
	for _, file := range analysis.Models {
		if file.Active {
			activeModels = append(activeModels, file.NodeID+":"+file.Name)
		} else {
			disabledModels = append(disabledModels, file.NodeID+":"+file.Name)
		}
	}
	assert.Equal(t, []string{
		"10:example_video_model_int8.safetensors",
		"11:example_style_lora.safetensors",
		"12:example_text_encoder.safetensors",
		"13:example_vae.safetensors",
	}, activeModels)
	assert.Equal(t, []string{"64:example_upscaler.safetensors"}, disabledModels)

	packages := map[string]bool{}
	for _, pkg := range analysis.Packages {
		packages[pkg.Name] = pkg.Active
	}
	assert.Equal(t, map[string]bool{
		"comfyui-workflow-encrypt":     true,
		"example/ComfyUI-ExampleVideo": true,
		"example/ComfyUI-Upscaler":     false,
		"unknown (Text)":               true,
	}, packages)

	outputs := map[string]string{}
	for _, output := range analysis.Outputs {
		outputs[output.NodeID] = output.Kind
	}
	assert.Equal(t, map[string]string{"40": KindVideo, "41": KindImage, "65": KindVideo}, outputs)
	assert.Equal(t, []string{"40"}, SuggestOutputNodes(analysis))
}

func TestAnalyzeWithoutEditorExport(t *testing.T) {
	analysis, err := Analyze(readFixture(t, "ref_video_api.json"), nil)
	require.NoError(t, err)
	assert.Zero(t, analysis.DisabledNodes)
	assert.Empty(t, candidatesByRole(analysis.Inputs, false))
	assert.Empty(t, analysis.Packages, "packages are only known from the editor export")
}

func TestSuggestMappingBindsActiveInputs(t *testing.T) {
	analysis, err := Analyze(readFixture(t, "ref_video_api.json"), readFixture(t, "ref_video_ui.json"))
	require.NoError(t, err)
	mapping := SuggestMapping(analysis)

	byRole := map[string]InputBinding{}
	for _, binding := range mapping.Inputs {
		byRole[binding.Role] = binding
	}
	require.Len(t, mapping.Inputs, 6)
	assert.Equal(t, InputBinding{Role: RolePrompt, NodeID: "21", Field: "text"}, byRole[RolePrompt])
	assert.Equal(t, "float", byRole[RoleDuration].ValueType)
	assert.Equal(t, "9:16 (Portrait Widescreen)", byRole[RoleAspectRatio].Enum["9:16"])
	assert.Equal(t, 1, mapping.MediaCapacity()[RoleImage])

	workflow, err := ParseAPIWorkflow(readFixture(t, "ref_video_api.json"))
	require.NoError(t, err)
	require.NoError(t, mapping.Validate(workflow))
}

func TestParseAPIWorkflowAcceptsStringWrappedJSONAndRejectsEditorExport(t *testing.T) {
	wrapped := []byte(`"{\"5\":{\"class_type\":\"SaveImage\",\"inputs\":{}}}"`)
	workflow, err := ParseAPIWorkflow(wrapped)
	require.NoError(t, err)
	assert.Contains(t, workflow, "5")

	_, err = ParseAPIWorkflow(readFixture(t, "ref_video_ui.json"))
	require.ErrorContains(t, err, "Export (API)")
}

// TestAnalyzeRealWorkflowExport checks the analyzer against a real
// RunningHub export kept outside the repository. Set
// MEDIA_WORKFLOW_FIXTURE_DIR to a folder with h3_api.json and h3_ui.json.
func TestAnalyzeRealWorkflowExport(t *testing.T) {
	dir := os.Getenv("MEDIA_WORKFLOW_FIXTURE_DIR")
	if dir == "" {
		t.Skip("MEDIA_WORKFLOW_FIXTURE_DIR not set")
	}
	apiJSON, err := os.ReadFile(filepath.Join(dir, "h3_api.json"))
	require.NoError(t, err)
	uiJSON, err := os.ReadFile(filepath.Join(dir, "h3_ui.json"))
	require.NoError(t, err)

	analysis, err := Analyze(apiJSON, uiJSON)
	require.NoError(t, err)
	assert.Equal(t, 20, analysis.ActiveNodes)
	assert.Equal(t, 32, analysis.DisabledNodes)
	assert.Equal(t, KindVideo, analysis.MediaKind)
	assert.Equal(t, map[string][]string{
		RolePrompt:      {"263.text"},
		RoleImage:       {"51.image"},
		RoleDuration:    {"259.value"},
		RoleAspectRatio: {"252.aspect_ratio"},
		RoleSeed:        {"256.noise_seed"},
	}, candidatesByRole(analysis.Inputs, true))
	disabled := candidatesByRole(analysis.Inputs, false)
	assert.Len(t, disabled[RoleImage], 8)
	assert.Len(t, disabled[RoleVideo], 3)
	assert.Len(t, disabled[RoleAudio], 3)
	assert.Equal(t, []string{"264"}, SuggestOutputNodes(analysis))
}

func TestAnalyzeWithoutPackagesReturnsEmptyLists(t *testing.T) {
	analysis, err := Analyze(readFixture(t, "ref_video_api.json"), nil)
	require.NoError(t, err)
	// The admin page reads these as arrays; nil would be sent as null.
	assert.NotNil(t, analysis.Packages)
	assert.NotNil(t, analysis.Models)
	assert.NotNil(t, analysis.Inputs)
	assert.NotNil(t, analysis.Outputs)
	assert.NotNil(t, analysis.Nodes)
}
