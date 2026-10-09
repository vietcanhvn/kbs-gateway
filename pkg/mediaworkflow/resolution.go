package mediaworkflow

import (
	"math"
	"strconv"
	"strings"
)

// Resolution roles: workflows that size their frames themselves (a
// ResolutionSelector "megapixels" widget, an Easy-Media "resolution.megapixels"
// field, a PrimitiveInt long edge feeding a resize node). The binding's Value
// is the workflow's own setting and stands for 720p; a request's resolution
// tier scales it by pixel count, so the workflow default stays as it was.
const (
	RoleMegapixels = "megapixels"
	RoleLongEdge   = "long_edge"
)

// Short side of a 16:9 frame at each tier the clients send.
var resolutionTierShortSide = map[string]float64{
	"360p":  360,
	"480p":  480,
	"720p":  720,
	"768p":  720,
	"1080p": 1080,
	"1k":    1080,
	"2k":    1440,
	"1440p": 1440,
	"4k":    2160,
	"2160p": 2160,
}

// ResolutionPixelRatio is the tier's pixel count relative to 720p (1 for an
// empty or unknown tier).
func ResolutionPixelRatio(tier string) float64 {
	side, ok := resolutionTierShortSide[strings.ToLower(strings.TrimSpace(tier))]
	if !ok {
		return 1
	}
	return (side * side) / (720 * 720)
}

// KnownResolution reports whether tier is one the resolution roles understand.
func KnownResolution(tier string) bool {
	_, ok := resolutionTierShortSide[strings.ToLower(strings.TrimSpace(tier))]
	return ok
}

// ResolutionScale is how much larger than its 720p setting the workflow
// renders a request's tier (pixel ratio, after the bindings' min / max): 1
// when the workflow has no resolution input or the request names no tier.
func (m InputMapping) ResolutionScale(tier string) float64 {
	scale := 0.0
	for _, binding := range m.Inputs {
		if binding.Role != RoleMegapixels && binding.Role != RoleLongEdge {
			continue
		}
		value, ok := resolutionValue(binding, MediaRequest{Resolution: tier})
		if !ok {
			continue
		}
		base, hasBase := numberOf(binding.Value)
		if !hasBase || base <= 0 {
			base = map[string]float64{RoleMegapixels: 1280 * 720 / 1e6, RoleLongEdge: 1280}[binding.Role]
		}
		got, _ := numberOf(value)
		ratio := got / base
		if binding.Role == RoleLongEdge {
			ratio *= ratio
		}
		scale = math.Max(scale, ratio)
	}
	return math.Max(scale, 1)
}

// HasResolutionRole reports whether the workflow lets requests pick a resolution.
func (m InputMapping) HasResolutionRole() bool {
	return m.HasRole(RoleMegapixels) || m.HasRole(RoleLongEdge)
}

// resolutionValue is the value a resolution binding gets for the request's
// tier, or false when the request names no (known) tier.
func resolutionValue(binding InputBinding, req MediaRequest) (any, bool) {
	if !KnownResolution(req.Resolution) {
		return nil, false
	}
	ratio := ResolutionPixelRatio(req.Resolution)
	base, hasBase := numberOf(binding.Value)
	var value float64
	switch binding.Role {
	case RoleMegapixels:
		if !hasBase || base <= 0 {
			base = 1280 * 720 / 1e6
		}
		value = math.Round(base*ratio*100) / 100
	case RoleLongEdge:
		if !hasBase || base <= 0 {
			base = 1280
		}
		value = math.Round(base*math.Sqrt(ratio)/32) * 32
	default:
		return nil, false
	}
	value = clamp(value, binding.Min, binding.Max)
	if binding.Role == RoleMegapixels && binding.ValueType != "int" {
		return value, true // megapixels are fractional (0.92 MP)
	}
	return numberForBinding(binding, value), true
}

func numberOf(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
		return parsed, err == nil
	}
	return 0, false
}
