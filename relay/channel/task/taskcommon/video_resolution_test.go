package taskcommon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeVideoResolutionLabels(t *testing.T) {
	cases := map[string]string{
		"480p":  VideoTier480p,
		"480P":  VideoTier480p,
		"540p":  VideoTier480p,
		"720p":  VideoTier720p,
		"768P":  VideoTier720p,
		"1080P": VideoTier1080p,
		"2K":    VideoTier2K,
		"1440p": VideoTier2K,
		"4k":    VideoTier4K,
		"2160p": VideoTier4K,
		"FHD":   VideoTier1080p,
	}
	for label, want := range cases {
		assert.Equal(t, want, NormalizeVideoResolution(label, 0, 0), label)
	}
}

func TestNormalizeVideoResolutionFallsBackToFrameSize(t *testing.T) {
	// Portrait: the short side decides.
	assert.Equal(t, VideoTier720p, NormalizeVideoResolution("", 720, 1280))
	assert.Equal(t, VideoTier1080p, NormalizeVideoResolution("", 1920, 1080))
	assert.Equal(t, VideoTier4K, NormalizeVideoResolution("weird", 3840, 2160))
	assert.Equal(t, "", NormalizeVideoResolution("", 0, 0))
}

func TestVideoResolutionPixelRatio(t *testing.T) {
	assert.InDelta(t, 0.4448, VideoResolutionPixelRatio(VideoTier480p), 0.001)
	assert.Equal(t, 1.0, VideoResolutionPixelRatio(VideoTier720p))
	assert.Equal(t, 2.25, VideoResolutionPixelRatio(VideoTier1080p))
	assert.Equal(t, 4.0, VideoResolutionPixelRatio(VideoTier2K))
	assert.Equal(t, 9.0, VideoResolutionPixelRatio(VideoTier4K))
	assert.Equal(t, 1.0, VideoResolutionPixelRatio("unknown"))
}
