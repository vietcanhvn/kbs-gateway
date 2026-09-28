package taskcommon

import (
	"regexp"
	"strconv"
	"strings"
)

// Shared video resolution tiers for billing.
//
// Providers name resolutions differently ("720p", "768P", "1080P", "2K",
// "1440p", "4k", "2160p", or only width/height). Every video channel maps its
// request onto one of five tiers so prices line up across models:
//
//	480p · 720p · 1080p · 2k · 4k
//
// VideoResolutionPixelRatio gives each tier's cost relative to 720p by pixel
// count (16:9 frame). Token-billed models (Seedance: tokens = W×H×FPS×s/1024)
// already scale this way, so per-call models priced "per second at 720p" and
// multiplied by this ratio stay comparable to them at every tier.
const (
	VideoTier480p  = "480p"
	VideoTier720p  = "720p"
	VideoTier1080p = "1080p"
	VideoTier2K    = "2k"
	VideoTier4K    = "4k"
)

// Pixel counts of a 16:9 frame at each tier.
var videoTierPixels = map[string]float64{
	VideoTier480p:  854 * 480,
	VideoTier720p:  1280 * 720,
	VideoTier1080p: 1920 * 1080,
	VideoTier2K:    2560 * 1440,
	VideoTier4K:    3840 * 2160,
}

var resolutionDigits = regexp.MustCompile(`\d+`)

// tierFromShortSide maps the short side of a frame (or a "NNNp" label) to a tier.
func tierFromShortSide(side int) string {
	switch {
	case side <= 0:
		return ""
	case side < 600:
		return VideoTier480p
	case side < 900: // 720p and 768p
		return VideoTier720p
	case side < 1300:
		return VideoTier1080p
	case side < 1800: // 1440p
		return VideoTier2K
	default:
		return VideoTier4K
	}
}

// NormalizeVideoResolution maps a provider resolution label, or the frame
// size when the label is empty or unknown, to one of the five tiers. Returns
// "" when neither says anything.
func NormalizeVideoResolution(label string, width, height int) string {
	value := strings.ToLower(strings.TrimSpace(label))
	switch value {
	case "2k", "qhd":
		return VideoTier2K
	case "4k", "uhd":
		return VideoTier4K
	case "hd":
		return VideoTier720p
	case "fhd", "fullhd", "full hd":
		return VideoTier1080p
	}
	if digits := resolutionDigits.FindString(value); digits != "" && strings.HasSuffix(value, "p") {
		if side, err := strconv.Atoi(digits); err == nil {
			return tierFromShortSide(side)
		}
	}
	if width > 0 && height > 0 {
		short := width
		if height < short {
			short = height
		}
		return tierFromShortSide(short)
	}
	return ""
}

// VideoResolutionPixelRatio is the tier's pixel count relative to 720p
// (480p ≈ 0.44, 720p = 1, 1080p = 2.25, 2k = 4, 4k = 9). Unknown tiers
// cost the same as 720p.
func VideoResolutionPixelRatio(tier string) float64 {
	pixels, ok := videoTierPixels[tier]
	if !ok {
		return 1
	}
	return pixels / videoTierPixels[VideoTier720p]
}
