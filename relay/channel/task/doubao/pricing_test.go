package doubao

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSeedanceFamilyFromGatewayNames(t *testing.T) {
	assert.Equal(t, "seedance-2.0", seedanceFamily("byteplus-seedance-2.0"))
	assert.Equal(t, "seedance-2.0", seedanceFamily("seedance-2.0-reference"))
	assert.Equal(t, "seedance-2.0", seedanceFamily("dreamina-seedance-2-0-260128"))
	assert.Equal(t, "seedance-2.0-fast", seedanceFamily("byteplus-seedance-2.0-fast"))
	assert.Equal(t, "seedance-2.5", seedanceFamily("seedance-2.5-pro"))
	assert.Equal(t, "", seedanceFamily("seedance-v1-pro-fast"))
	assert.Equal(t, "", seedanceFamily("minimax-h3"))
}

func TestBytePlusSeedanceRatiosFollowTheListPrices(t *testing.T) {
	cases := []struct {
		model      string
		resolution string
		hasVideo   bool
		want       float64
	}{
		{"byteplus-seedance-2.0", "480p", false, 1},
		{"byteplus-seedance-2.0", "720p", false, 1},
		{"byteplus-seedance-2.0", "1080p", false, 0.0077 / 0.007},
		{"byteplus-seedance-2.0", "4k", false, 0.004 / 0.007},
		{"byteplus-seedance-2.0", "720p", true, 0.0043 / 0.007},
		{"byteplus-seedance-2.0", "1080p", true, 0.0047 / 0.007},
		{"seedance-2.0-fast", "720p", true, 0.0033 / 0.0056},
		{"seedance-2.5", "1080p", false, 0.0117 / 0.0107},
	}
	for _, c := range cases {
		ratio, ok := GetVideoInputRatio(c.model, c.resolution, c.hasVideo)
		assert.True(t, ok, c.model)
		assert.InDelta(t, c.want, ratio, 1e-9, "%s %s video=%v", c.model, c.resolution, c.hasVideo)
	}
}

func TestVolcengineIDsKeepTheirOwnTable(t *testing.T) {
	ratio, ok := GetVideoInputRatio("doubao-seedance-2-0-260128", "1080p", false)
	assert.True(t, ok)
	assert.InDelta(t, 51.0/46.0, ratio, 1e-9)
}

func TestUnpricedModelsAreLeftAlone(t *testing.T) {
	_, ok := GetVideoInputRatio("seedance-v1-pro-fast", "720p", false)
	assert.False(t, ok)
}
