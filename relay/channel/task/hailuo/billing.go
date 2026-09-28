package hailuo

import (
	"github.com/gin-gonic/gin"

	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// EstimateBilling prices MiniMax-H3 by duration and resolution.
//
// The model's configured per-call price is read as "USD per second at 720p";
// the charge is price × seconds × resolution ratio, with the ratio taken from
// the shared tiers (768P bills as 720p, 2K as 4× 720p). H3 is priced as a
// multiple of Seedance, whose tokens scale with pixels × seconds, so using
// the same pixel ratio keeps that multiple at every resolution.
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
	return map[string]float64{
		"seconds":    float64(h3Req.Duration),
		"resolution": taskcommon.VideoResolutionPixelRatio(tier),
	}
}
