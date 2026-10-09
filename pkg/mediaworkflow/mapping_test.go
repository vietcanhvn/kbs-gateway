package mediaworkflow

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func float(value float64) *float64 { return &value }

func referenceVideoMapping() InputMapping {
	return InputMapping{Inputs: []InputBinding{
		{Role: RolePrompt, NodeID: "21", Field: "text"},
		{Role: RoleNegativePrompt, NodeID: "22", Field: "text"},
		{Role: RoleImage, Index: 0, NodeID: "20", Field: "image", Required: true},
		{Role: RoleImage, Index: 1, NodeID: "60", Field: "image"},
		{Role: RoleVideo, Index: 0, NodeID: "62", Field: "video"},
		{Role: RoleDuration, NodeID: "23", Field: "value", ValueType: "float", Min: float(4), Max: float(15)},
		{Role: RoleAspectRatio, NodeID: "24", Field: "aspect_ratio", Enum: map[string]string{
			"16:9": "16:9 (Widescreen)", "9:16": "9:16 (Portrait Widescreen)", "1:1": "1:1 (Square)",
		}},
		{Role: RoleSeed, NodeID: "25", Field: "noise_seed"},
		{Role: RoleFixed, NodeID: "40", Field: "frame_rate", Value: 24},
	}}
}

func TestBuildNodeOverridesFromRequest(t *testing.T) {
	overrides, err := BuildNodeOverrides(referenceVideoMapping(), MediaRequest{
		Prompt:   "<Picture 1> walks",
		Images:   []string{"api/a.png", "api/b.png"},
		Videos:   []string{"api/c.mp4"},
		Duration: 20,
		Width:    1280,
		Height:   720,
		Seed:     7,
	})
	require.NoError(t, err)
	assert.Equal(t, []NodeOverride{
		{NodeID: "21", FieldName: "text", FieldValue: "<Picture 1> walks"},
		{NodeID: "20", FieldName: "image", FieldValue: "api/a.png"},
		{NodeID: "60", FieldName: "image", FieldValue: "api/b.png"},
		{NodeID: "62", FieldName: "video", FieldValue: "api/c.mp4"},
		{NodeID: "23", FieldName: "value", FieldValue: 15.0},
		{NodeID: "24", FieldName: "aspect_ratio", FieldValue: "16:9 (Widescreen)"},
		{NodeID: "25", FieldName: "noise_seed", FieldValue: int64(7)},
		{NodeID: "40", FieldName: "frame_rate", FieldValue: 24},
	}, overrides, "empty negative prompt keeps the workflow default; duration is clamped to max 15")
}

func TestBuildNodeOverridesAspectRatio(t *testing.T) {
	tests := []struct {
		name    string
		request MediaRequest
		want    any
	}{
		{name: "explicit ratio", request: MediaRequest{Ratio: "9:16"}, want: "9:16 (Portrait Widescreen)"},
		{name: "ratio from size", request: MediaRequest{Width: 1080, Height: 1920}, want: "9:16 (Portrait Widescreen)"},
		{name: "closest enum entry", request: MediaRequest{Ratio: "5:4"}, want: "1:1 (Square)"},
		{name: "wide closest", request: MediaRequest{Ratio: "21:9"}, want: "16:9 (Widescreen)"},
		{name: "auto keeps workflow value", request: MediaRequest{Ratio: "auto"}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.request.Images = []string{"api/a.png"}
			overrides, err := BuildNodeOverrides(referenceVideoMapping(), tt.request)
			require.NoError(t, err)
			var got any
			for _, override := range overrides {
				if override.NodeID == "24" {
					got = override.FieldValue
				}
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildNodeOverridesRandomSeedWhenMissing(t *testing.T) {
	overrides, err := BuildNodeOverrides(InputMapping{Inputs: []InputBinding{{Role: RoleSeed, NodeID: "25", Field: "noise_seed"}}}, MediaRequest{})
	require.NoError(t, err)
	require.Len(t, overrides, 1)
	seed, ok := overrides[0].FieldValue.(int64)
	require.True(t, ok)
	assert.Positive(t, seed)
}

func TestBuildNodeOverridesRejectsUnsupportedRequests(t *testing.T) {
	tests := []struct {
		name       string
		mapping    InputMapping
		request    MediaRequest
		request400 bool
		message    string
	}{
		{name: "too many images", mapping: referenceVideoMapping(), request: MediaRequest{Images: []string{"a", "b", "c"}}, request400: true, message: "at most 2 image"},
		{name: "audio not accepted", mapping: referenceVideoMapping(), request: MediaRequest{Images: []string{"a"}, Audios: []string{"x"}}, request400: true, message: "at most 0 audio"},
		{name: "required image missing", mapping: referenceVideoMapping(), request: MediaRequest{Prompt: "hi"}, request400: true, message: "image 1 is required"},
		{name: "no last frame input", mapping: referenceVideoMapping(), request: MediaRequest{Images: []string{"a"}, LastFrame: "z"}, request400: true, message: "no last-frame input"},
		{name: "prompt without prompt node", mapping: InputMapping{}, request: MediaRequest{Prompt: "hi"}, message: "no prompt input mapped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildNodeOverrides(tt.mapping, tt.request)
			require.ErrorContains(t, err, tt.message)
			var requestErr *RequestError
			assert.Equal(t, tt.request400, errors.As(err, &requestErr))
		})
	}
}

func TestInputMappingValidate(t *testing.T) {
	workflow := map[string]map[string]any{"21": {"class_type": "Text"}}
	require.NoError(t, InputMapping{Inputs: []InputBinding{{Role: RolePrompt, NodeID: "21", Field: "text"}}}.Validate(workflow))
	require.ErrorContains(t, InputMapping{Inputs: []InputBinding{{Role: RolePrompt, NodeID: "99", Field: "text"}}}.Validate(workflow), "not an active node")
	require.ErrorContains(t, InputMapping{Inputs: []InputBinding{{Role: "colour", NodeID: "21", Field: "text"}}}.Validate(workflow), "unknown input role")
	require.ErrorContains(t, InputMapping{Inputs: []InputBinding{
		{Role: RolePrompt, NodeID: "21", Field: "text"}, {Role: RoleNegativePrompt, NodeID: "21", Field: "text"},
	}}.Validate(workflow), "bound twice")
	require.ErrorContains(t, InputMapping{Billing: "per_frame"}.Validate(workflow), "billing")
}

func TestRequestRatioAndParseSize(t *testing.T) {
	assert.Equal(t, "16:9", RequestRatio("", 1280, 720))
	assert.Equal(t, "", RequestRatio("adaptive", 1280, 720))
	assert.Equal(t, "3:4", RequestRatio("3:4", 0, 0))
	width, height := ParseSize("1920x1080")
	assert.Equal(t, []int{1920, 1080}, []int{width, height})
	width, height = ParseSize("1024*1024")
	assert.Equal(t, []int{1024, 1024}, []int{width, height})
	width, height = ParseSize("auto")
	assert.Equal(t, []int{0, 0}, []int{width, height})
}

func TestUnwireUnusedMediaDisconnectsEmptySlots(t *testing.T) {
	workflow, err := ParseAPIWorkflow([]byte(`{
		"51": {"class_type": "LoadImage", "inputs": {"image": "a.png"}},
		"49": {"class_type": "LoadImage", "inputs": {"image": "b.png"}},
		"48": {"class_type": "LoadAudio", "inputs": {"audio": "c.wav"}},
		"265": {"class_type": "MiniMaxH3ReferenceToVideo", "inputs": {
			"ref_images.ref_image_0": ["51", 0], "ref_images.ref_image_1": ["49", 0], "ref_audios.ref_audio_0": ["48", 0]}}
	}`))
	require.NoError(t, err)
	mapping := InputMapping{Inputs: []InputBinding{
		{Role: RoleImage, Index: 0, NodeID: "51", Field: "image", Required: true},
		{Role: RoleImage, Index: 1, NodeID: "49", Field: "image"},
		{Role: RoleAudio, Index: 0, NodeID: "48", Field: "audio"},
	}}

	got := UnwireUnusedMedia(mapping, workflow, MediaRequest{Images: []string{"x.png"}})
	assert.Equal(t, []NodeOverride{
		{NodeID: "265", FieldName: "ref_audios.ref_audio_0", FieldValue: nil},
		{NodeID: "265", FieldName: "ref_images.ref_image_1", FieldValue: nil},
	}, got)

	assert.Empty(t, UnwireUnusedMedia(mapping, workflow, MediaRequest{Images: []string{"x", "y"}, Audios: []string{"z"}}), "every slot used")
	mapping.UnusedMedia = "keep"
	assert.Empty(t, UnwireUnusedMedia(mapping, workflow, MediaRequest{Images: []string{"x"}}), "keep leaves sample files")
}

func TestUnwireUnusedMediaDisconnectsAnEmptyLastFrame(t *testing.T) {
	workflow, err := ParseAPIWorkflow([]byte(`{
		"9": {"class_type": "LoadImage", "inputs": {"image": "first.png"}},
		"54": {"class_type": "LoadImage", "inputs": {"image": "last.png"}},
		"16": {"class_type": "MiniMaxH3ImageToVideo", "inputs": {"first_frame": ["9", 0], "last_frame": ["54", 0]}}
	}`))
	require.NoError(t, err)
	mapping := InputMapping{Inputs: []InputBinding{
		{Role: RoleImage, Index: 0, NodeID: "9", Field: "image", Required: true},
		{Role: RoleLastFrame, NodeID: "54", Field: "image"},
	}}
	assert.Equal(t, []NodeOverride{{NodeID: "16", FieldName: "last_frame", FieldValue: nil}},
		UnwireUnusedMedia(mapping, workflow, MediaRequest{Images: []string{"x"}}))
	assert.Empty(t, UnwireUnusedMedia(mapping, workflow, MediaRequest{Images: []string{"x"}, LastFrame: "y"}))
}

func TestUnwireUnusedMediaFollowsResizeHelpers(t *testing.T) {
	workflow, err := ParseAPIWorkflow([]byte(`{
		"9": {"class_type": "LoadImage", "inputs": {"image": "first.png"}},
		"15": {"class_type": "ResolutionSelector", "inputs": {"aspect_ratio": "16:9"}},
		"54": {"class_type": "LoadImage", "inputs": {"image": "last.png"}},
		"55": {"class_type": "ImageResizeKJv2", "inputs": {"image": ["54", 0], "width": ["15", 0], "height": ["15", 1]}},
		"16": {"class_type": "MiniMaxH3ImageToVideo", "inputs": {"first_frame": ["9", 0], "last_frame": ["55", 0], "width": ["15", 0]}}
	}`))
	require.NoError(t, err)
	mapping := InputMapping{Inputs: []InputBinding{
		{Role: RoleImage, Index: 0, NodeID: "9", Field: "image", Required: true},
		{Role: RoleLastFrame, NodeID: "54", Field: "image"},
	}}
	assert.Equal(t, []NodeOverride{{NodeID: "16", FieldName: "last_frame", FieldValue: nil}},
		UnwireUnusedMedia(mapping, workflow, MediaRequest{Images: []string{"x"}}))
}
