package comfyui

import (
	"github.com/gin-gonic/gin"

	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// defaultH3Seconds is H3's own default length, used when a request names none.
const defaultH3Seconds = 5

// EstimateBilling prices self-hosted MiniMax-H3 workflows like the MiniMax API
// channel: the model price is read as USD per second at 768P and multiplied by
// seconds × resolution ratio (taskcommon.MiniMaxH3ResolutionPriceRatio). The
// resolution comes from the requested label or the width × height sent to the
// workflow. Other ComfyUI workflows (images, other video models) keep a flat
// per-call price.
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if !taskcommon.IsMiniMaxH3Model(info.OriginModelName, info.UpstreamModelName) {
		return nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	var metadata comfyMetadata
	_ = taskcommon.UnmarshalMetadata(req.Metadata, &metadata)

	seconds := firstNonZero(req.Duration, metadata.Duration)
	if seconds <= 0 {
		seconds = defaultH3Seconds
	}
	tier := taskcommon.NormalizeVideoResolution(
		firstNonEmpty(metadata.Resolution, req.Size),
		firstNonZero(req.Width, metadata.Width),
		firstNonZero(req.Height, metadata.Height),
	)
	return map[string]float64{
		"seconds":    float64(seconds),
		"resolution": taskcommon.MiniMaxH3ResolutionPriceRatio(tier),
	}
}
