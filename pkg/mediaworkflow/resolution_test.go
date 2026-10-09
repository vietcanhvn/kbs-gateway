package mediaworkflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolutionRolesScaleTheWorkflowDefault(t *testing.T) {
	maxMP := 4.0
	mapping := InputMapping{Inputs: []InputBinding{
		{Role: RoleMegapixels, NodeID: "29", Field: "resolution.megapixels", ValueType: "float", Value: 1.0, Max: &maxMP},
		{Role: RoleMegapixels, NodeID: "22", Field: "megapixels", Value: "0.2"},
		{Role: RoleLongEdge, NodeID: "213", Field: "value", Value: 1280},
	}}
	values := func(tier string) map[string]any {
		overrides, err := BuildNodeOverrides(mapping, MediaRequest{Resolution: tier})
		require.NoError(t, err)
		out := map[string]any{}
		for _, override := range overrides {
			out[override.NodeID] = override.FieldValue
		}
		return out
	}
	assert.Equal(t, map[string]any{"29": 1.0, "22": 0.2, "213": int64(1280)}, values("720p"), "720p keeps the workflow's own size")
	assert.Equal(t, map[string]any{"29": 2.25, "22": 0.45, "213": int64(1920)}, values("1080p"))
	assert.Equal(t, map[string]any{"29": 0.25, "22": 0.05, "213": int64(640)}, values("360p"))
	assert.Equal(t, 4.0, values("4k")["29"], "clamped to the binding maximum")
	assert.Empty(t, values(""), "no tier: leave the workflow as it is")
	assert.True(t, mapping.HasResolutionRole())
	assert.InDelta(t, 2.25, ResolutionPixelRatio("1080p"), 0.001)
	assert.Equal(t, 1.0, ResolutionPixelRatio("weird"))
}

func TestAnalyzeFindsMegapixelsInputs(t *testing.T) {
	api := []byte(`{
"15":{"class_type":"ResolutionSelector","inputs":{"aspect_ratio":"16:9 (Widescreen)","megapixels":["27",0]}},
"22":{"class_type":"ResolutionSelector","inputs":{"aspect_ratio":"16:9 (Widescreen)","megapixels":0.2}},
"27":{"class_type":"PrimitiveFloat","inputs":{"value":1}},
"29":{"class_type":"easy multiTrackEditor","inputs":{"resolution":"width x height (megapixels)","resolution.megapixels":1,"resolution.aspect_ratio":"16:9 (Widescreen)"}}
}`)
	analysis, err := Analyze(api, nil)
	require.NoError(t, err)
	mapping := SuggestMapping(analysis)
	found := map[string]InputBinding{}
	for _, binding := range mapping.Inputs {
		found[binding.Role+"@"+binding.NodeID+"."+binding.Field] = binding
	}
	assert.Contains(t, found, "megapixels@27.value")
	assert.Contains(t, found, "megapixels@22.megapixels")
	assert.Contains(t, found, "megapixels@29.resolution.megapixels")
	assert.Equal(t, "float", found["megapixels@27.value"].ValueType)
	assert.EqualValues(t, 1, found["megapixels@27.value"].Value)
}
