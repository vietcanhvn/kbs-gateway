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
	// ComfyUI rejects megapixels below 0.1: the 0.2 MP pass floors at 0.1 (was 0.05 / 0.09 -> RunningHub 433).
	assert.Equal(t, map[string]any{"29": 0.25, "22": 0.1, "213": int64(640)}, values("360p"))
	assert.Equal(t, 0.1, values("480p")["22"], "480p: 0.2*0.44=0.09 floors to 0.1")
	assert.Equal(t, 0.44, values("480p")["29"])
	assert.Equal(t, 4.0, values("4k")["29"], "clamped to the binding maximum")
	assert.Empty(t, values(""), "no tier: leave the workflow as it is")
	assert.True(t, mapping.HasResolutionRole())
	assert.InDelta(t, 2.25, mapping.ResolutionScale("1080p"), 0.01)
	capped := InputMapping{Inputs: mapping.Inputs[:1]}
	assert.InDelta(t, 4.0, capped.ResolutionScale("4k"), 0.01, "billed as rendered: the 4 MP cap, not 4K")
	assert.Equal(t, 1.0, mapping.ResolutionScale("360p"), "smaller tiers bill as 720p")
	assert.Equal(t, 1.0, InputMapping{}.ResolutionScale("4k"), "no resolution input: workflow size, no surcharge")
	assert.InDelta(t, 2.25, ResolutionPixelRatio("1080p"), 0.001)
	assert.Equal(t, 1.0, ResolutionPixelRatio("weird"))
}

// RunningHub bills machine time, not pixels: a workflow can carry its measured
// multiplier per tier (MiniMax H3 long take 480p ~0.31x of 720p, 1080p ~3x).
func TestResolutionPricesOverrideThePixelScale(t *testing.T) {
	maxMP := 2.25
	mapping := InputMapping{
		Inputs:           []InputBinding{{Role: RoleMegapixels, NodeID: "29", Field: "resolution.megapixels", ValueType: "float", Value: 1.0, Max: &maxMP}},
		ResolutionPrices: map[string]float64{"480p": 0.31, "1080p": 3.2},
	}
	require.NoError(t, mapping.Validate(nil))
	assert.Equal(t, 0.31, mapping.ResolutionPriceRatio("480p"), "measured tier below 720p bills less")
	assert.Equal(t, 3.2, mapping.ResolutionPriceRatio("1080P "), "tier names ignore case and spaces")
	assert.Equal(t, 1.0, mapping.ResolutionPriceRatio("720p"))
	assert.Equal(t, 1.0, mapping.ResolutionPriceRatio("360p"), "unlisted smaller tier keeps the 720p price")
	assert.InDelta(t, 2.25, mapping.ResolutionPriceRatio("4k"), 0.01, "unlisted larger tier: pixel scale, capped by the binding max")
	assert.Equal(t, 1.0, mapping.ResolutionPriceRatio(""), "no tier: workflow default")
	assert.Equal(t, 1.0, InputMapping{ResolutionPrices: map[string]float64{"480p": 0.3}}.ResolutionPriceRatio("480p"), "no resolution input: the size never changes")

	bad := mapping
	bad.ResolutionPrices = map[string]float64{"8k": 2}
	assert.ErrorContains(t, bad.Validate(nil), "unknown tier")
	bad.ResolutionPrices = map[string]float64{"480p": 0}
	assert.ErrorContains(t, bad.Validate(nil), "above 0")
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

// KBS 12/11: a picture workflow's default (2 MP) is a 1K picture; 2K / 4K scale
// from there, and a request without a tier keeps the default.
func TestBaseResolutionForPictureWorkflows(t *testing.T) {
	maxMP := 8.3
	mapping := InputMapping{
		Inputs:           []InputBinding{{Role: RoleMegapixels, NodeID: "7", Field: "megapixels", ValueType: "float", Value: 2.0, Max: &maxMP, BaseResolution: "1k"}},
		Billing:          "per_call",
		ResolutionPrices: map[string]float64{"2k": 1.6, "4k": 3.2},
	}
	megapixels := func(tier string) any {
		overrides, err := BuildNodeOverrides(mapping, MediaRequest{Resolution: tier})
		require.NoError(t, err)
		for _, override := range overrides {
			if override.NodeID == "7" {
				return override.FieldValue
			}
		}
		return nil
	}
	assert.Nil(t, megapixels(""), "no tier: the workflow default (2 MP) stays")
	assert.Equal(t, 2.0, megapixels("1k"))
	assert.Equal(t, 3.56, megapixels("2k"))
	assert.Equal(t, 8.0, megapixels("4k"))
	assert.Equal(t, 1.0, mapping.ResolutionPriceRatio("1k"))
	assert.Equal(t, 1.6, mapping.ResolutionPriceRatio("2k"))
	assert.Equal(t, 3.2, mapping.ResolutionPriceRatio("4k"))
}
