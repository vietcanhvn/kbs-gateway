package comfyui

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func estimateComfy(t *testing.T, model string, req relaycommon.TaskSubmitReq) map[string]float64 {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("task_request", req)
	info := &relaycommon.RelayInfo{OriginModelName: model, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: model}}
	return (&TaskAdaptor{}).EstimateBilling(ctx, info)
}

func TestSelfHostedH3BillsBySecondsAndFrameSize(t *testing.T) {
	// KSB sends width × height; 864×480 is the 480p tier.
	ratios := estimateComfy(t, "minimax-h3", relaycommon.TaskSubmitReq{
		Model: "minimax-h3", Prompt: "x", Duration: 6, Width: 864, Height: 480,
	})
	assert.Equal(t, 6.0, ratios["seconds"])
	assert.InDelta(t, 0.625, ratios["resolution"], 1e-9)

	ratios = estimateComfy(t, "minimax-h3-flf", relaycommon.TaskSubmitReq{
		Model: "minimax-h3-flf", Prompt: "x", Duration: 10, Width: 720, Height: 1280,
		Metadata: map[string]any{"resolution": "720p"},
	})
	assert.Equal(t, 10.0, ratios["seconds"])
	assert.Equal(t, 1.0, ratios["resolution"])
}

func TestSelfHostedH3DefaultsToFiveSeconds(t *testing.T) {
	ratios := estimateComfy(t, "minimax-h3", relaycommon.TaskSubmitReq{Model: "minimax-h3", Prompt: "x", Width: 1344, Height: 768})
	assert.Equal(t, 5.0, ratios["seconds"])
	assert.Equal(t, 1.0, ratios["resolution"])
}

func TestOtherComfyWorkflowsKeepFlatPrice(t *testing.T) {
	assert.Nil(t, estimateComfy(t, "qwen-image-2.1", relaycommon.TaskSubmitReq{Model: "qwen-image-2.1", Prompt: "x", Width: 1024, Height: 1024}))
}
