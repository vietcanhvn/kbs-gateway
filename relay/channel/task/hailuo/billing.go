package hailuo

import (
	"github.com/gin-gonic/gin"

	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// EstimateBilling prices MiniMax-H3 by duration and resolution.
//
// The model's configured per-call price is read as "USD per second at 768P";
// the charge is price × seconds × resolution ratio. The ratio follows
// MiniMax's own price list (h3ResolutionPriceRatio: 2K = 1.625× 768P);
// a tier without a listed price falls back to the shared pixel ratio.
//
// The request fixes both duration (4-15 s) and resolution up front, so this
// estimate is also the final charge. Older Hailuo models keep a flat price.
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if !isH3Model(info.UpstreamModelName) {
		return nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	h3Req, err := h3VideoRequestFromTask(req)
	if err != nil || h3Req.Duration <= 0 {
		return nil
	}
	tier := taskcommon.NormalizeVideoResolution(h3Req.Resolution, 0, 0)
	ratio, ok := h3ResolutionPriceRatio[tier]
	if !ok {
		// A tier added to h3Resolutions before its price is known: bill by pixels.
		ratio = taskcommon.VideoResolutionPixelRatio(tier)
	}
	return map[string]float64{
		"seconds":    float64(h3Req.Duration),
		"resolution": ratio,
	}
}
