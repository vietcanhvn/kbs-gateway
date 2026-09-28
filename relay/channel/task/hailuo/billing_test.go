package hailuo

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func estimateFor(t *testing.T, upstreamModel string, req relaycommon.TaskSubmitReq) map[string]float64 {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("task_request", req)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: upstreamModel}}
	return (&TaskAdaptor{}).EstimateBilling(ctx, info)
}

func TestH3BillingBySecondsAndResolution(t *testing.T) {
	ratios := estimateFor(t, h3ModelName, relaycommon.TaskSubmitReq{
		Model:    h3ModelName,
		Prompt:   "A quiet street",
		Duration: 6,
		Metadata: map[string]any{"resolution": "720p", "ratio": "16:9"},
	})
	assert.Equal(t, 6.0, ratios["seconds"])
	assert.Equal(t, 1.0, ratios["resolution"]) // 768P bills as 720p

	ratios = estimateFor(t, h3ModelName, relaycommon.TaskSubmitReq{
		Model:    h3ModelName,
		Prompt:   "A quiet street",
		Duration: 10,
		Metadata: map[string]any{"resolution": "2k", "ratio": "16:9"},
	})
	assert.Equal(t, 10.0, ratios["seconds"])
	assert.InDelta(t, 1.625, ratios["resolution"], 1e-9)
}

func TestH3BillingDefaultsMatchTheRequestSentUpstream(t *testing.T) {
	// No duration / resolution given: H3 sends 5 s at 2K, so bill exactly that.
	ratios := estimateFor(t, h3ModelName, relaycommon.TaskSubmitReq{
		Model:    h3ModelName,
		Prompt:   "A quiet street",
		Metadata: map[string]any{"ratio": "16:9"},
	})
	assert.Equal(t, 5.0, ratios["seconds"])
	assert.InDelta(t, 1.625, ratios["resolution"], 1e-9)
}

func TestOlderHailuoModelsKeepFlatPrice(t *testing.T) {
	assert.Nil(t, estimateFor(t, "MiniMax-Hailuo-02", relaycommon.TaskSubmitReq{Model: "MiniMax-Hailuo-02", Prompt: "x", Duration: 6}))
}

func TestH3BillingSkipsInvalidRequests(t *testing.T) {
	// Rejected later by validation; billing must not invent a price.
	assert.Nil(t, estimateFor(t, h3ModelName, relaycommon.TaskSubmitReq{
		Model:    h3ModelName,
		Prompt:   "x",
		Duration: 30,
		Metadata: map[string]any{"ratio": "16:9"},
	}))
}

func TestH3ServesUnsupportedTiersAtTheNearestOne(t *testing.T) {
	cases := map[string]string{"480p": "768P", "720p": "768P", "1080p": "2K", "4k": "2K", "2160p": "2K"}
	for requested, want := range cases {
		got, err := normalizeH3Resolution(requested)
		assert.NoError(t, err, requested)
		assert.Equal(t, want, got, requested)
	}
	_, err := normalizeH3Resolution("banana")
	assert.Error(t, err)

	// Billing follows what is actually rendered: a 1080p request bills as 2K.
	ratios := estimateFor(t, h3ModelName, relaycommon.TaskSubmitReq{
		Model:    h3ModelName,
		Prompt:   "x",
		Duration: 4,
		Metadata: map[string]any{"resolution": "1080p", "ratio": "16:9"},
	})
	assert.InDelta(t, 1.625, ratios["resolution"], 1e-9)
}
