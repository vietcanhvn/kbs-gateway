package gemini

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// Veo 3.1 limits (Gemini API "Generate videos with Veo 3.1" and Vertex AI
// Veo reference docs):
//   - referenceImages: at most 3, referenceType "asset", Veo 3.1 / 3.1 Fast;
//     cannot be combined with image or lastFrame.
//   - lastFrame: Veo 3.1 only and must be sent together with image.
//   - durationSeconds must be 8 with referenceImages; DramaClaw also pins
//     interpolation (lastFrame) to 8 so billing and output stay predictable.
const (
	VeoMaxReferenceImages     = 3
	VeoFixedDurationSeconds   = 8
	veoReferenceTypeAsset     = "asset"
	veoMediaContextKey        = "veo_media_inputs"
	veoMediaErrInvalidRequest = "invalid_media_request"
)

// VeoMediaInputs is the resolved media for one Veo instance.
type VeoMediaInputs struct {
	Image           *VeoImageInput
	LastFrame       *VeoImageInput
	ReferenceImages []VeoReferenceImage
}

// RequiresFixedDuration reports whether Veo must run at 8 seconds.
func (m *VeoMediaInputs) RequiresFixedDuration() bool {
	return m != nil && (m.LastFrame != nil || len(m.ReferenceImages) > 0)
}

type veoMediaError struct {
	code    string
	message string
}

func (e *veoMediaError) Error() string { return e.message }

func newVeoMediaError(code, format string, args ...any) error {
	return &veoMediaError{code: code, message: fmt.Sprintf(format, args...)}
}

// fetchVeoImageURL downloads an http(s) image (SSRF-protected, size limited by
// MAX_FILE_DOWNLOAD_MB). Overridable in tests.
var fetchVeoImageURL = service.GetImageFromUrl

// ValidateVeoTaskRequest runs the shared DC-Media validation and then the
// Veo-specific media checks. Images are resolved here (before pre-consume) so
// bad media is a 400 instead of a billed upstream failure. Used by both the
// Gemini and Vertex task adaptors.
func ValidateVeoTaskRequest(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	media, err := ResolveVeoMedia(c, info, req)
	if err != nil {
		code := veoMediaErrInvalidRequest
		if mediaErr, ok := err.(*veoMediaError); ok {
			code = mediaErr.code
		}
		return service.TaskErrorWrapperLocal(err, code, http.StatusBadRequest)
	}
	c.Set(veoMediaContextKey, media)
	return nil
}

// ResolveVeoMedia maps the DC-Media request (image, metadata.last_frame_image,
// metadata.reference_images) onto Veo instance media and enforces Veo rules.
func ResolveVeoMedia(c *gin.Context, info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (*VeoMediaInputs, error) {
	dc := relaycommon.DCMediaMetadata{}
	if err := req.UnmarshalMetadata(&dc); err != nil {
		return nil, newVeoMediaError(veoMediaErrInvalidRequest, "invalid metadata: %v", err)
	}
	if len(nonEmpty(dc.ReferenceVideos)) > 0 || len(nonEmpty(dc.ReferenceAudios)) > 0 {
		return nil, newVeoMediaError("unsupported_media_field", "veo does not support metadata.reference_videos or metadata.reference_audios")
	}
	if strings.TrimSpace(dc.ReferenceFile) != "" || strings.TrimSpace(dc.ReferenceLink) != "" {
		return nil, newVeoMediaError("unsupported_media_field", "veo does not support metadata.reference_file or metadata.reference_link")
	}

	refs := nonEmpty(dc.ReferenceImages)
	if len(refs) > VeoMaxReferenceImages {
		return nil, newVeoMediaError("too_many_reference_images", "veo accepts at most %d reference images, got %d", VeoMaxReferenceImages, len(refs))
	}
	lastRaw := strings.TrimSpace(dc.LastFrameImage)

	media := &VeoMediaInputs{}
	firstRaw := strings.TrimSpace(req.Image)
	if firstRaw == "" && len(req.Images) > 0 {
		firstRaw = strings.TrimSpace(req.Images[0])
	}
	var multipartImage *VeoImageInput
	if c != nil {
		multipartImage = ExtractMultipartImage(c, info)
	}
	hasFirst := multipartImage != nil || firstRaw != ""

	if len(refs) > 0 && (hasFirst || lastRaw != "") {
		return nil, newVeoMediaError("conflicting_media_inputs", "veo reference_images cannot be combined with image (first frame) or last_frame_image")
	}
	if lastRaw != "" && !hasFirst {
		return nil, newVeoMediaError("missing_first_frame", "veo last_frame_image requires image (first frame)")
	}

	modelName := ""
	if info != nil {
		modelName = info.UpstreamModelName
	}
	if len(refs) > 0 && !veoModelSupportsReferenceImages(modelName) {
		return nil, newVeoMediaError("unsupported_model_feature", "model %s does not support reference_images (Veo 3.1 / 3.1 Fast only)", modelName)
	}
	if lastRaw != "" && !veoModelSupportsLastFrame(modelName) {
		return nil, newVeoMediaError("unsupported_model_feature", "model %s does not support last_frame_image (Veo 3.1 only)", modelName)
	}

	var err error
	if multipartImage != nil {
		media.Image = multipartImage
	} else if firstRaw != "" {
		if media.Image, err = resolveVeoImage(firstRaw, "image"); err != nil {
			return nil, err
		}
	}
	if lastRaw != "" {
		if media.LastFrame, err = resolveVeoImage(lastRaw, "metadata.last_frame_image"); err != nil {
			return nil, err
		}
	}
	for i, raw := range refs {
		img, err := resolveVeoImage(raw, fmt.Sprintf("metadata.reference_images[%d]", i))
		if err != nil {
			return nil, err
		}
		media.ReferenceImages = append(media.ReferenceImages, VeoReferenceImage{Image: img, ReferenceType: veoReferenceTypeAsset})
	}
	if info != nil && (media.Image != nil || len(media.ReferenceImages) > 0) {
		info.Action = constant.TaskActionGenerate
	}
	return media, nil
}

// resolveVeoImage accepts a data URL, raw base64 or an http(s) URL.
func resolveVeoImage(raw, field string) (*VeoImageInput, error) {
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		mimeType, data, err := fetchVeoImageURL(raw)
		if err != nil {
			return nil, newVeoMediaError("invalid_media_url", "%s: failed to download image: %v", field, err)
		}
		if i := strings.Index(mimeType, ";"); i >= 0 {
			mimeType = strings.TrimSpace(mimeType[:i])
		}
		return &VeoImageInput{BytesBase64Encoded: data, MimeType: mimeType}, nil
	}
	img := ParseImageInput(raw)
	if img == nil {
		return nil, newVeoMediaError("invalid_media", "%s must be a data URL, base64 image or http(s) URL", field)
	}
	return img, nil
}

func veoModelSupportsReferenceImages(model string) bool {
	m := strings.ToLower(model)
	return !(strings.Contains(m, "veo-2") || strings.Contains(m, "veo-3.0") || strings.Contains(m, "lite"))
}

func veoModelSupportsLastFrame(model string) bool {
	m := strings.ToLower(model)
	return !(strings.Contains(m, "veo-2") || strings.Contains(m, "veo-3.0"))
}

// veoRequestNeedsFixedDuration is the metadata-only version of
// VeoMediaInputs.RequiresFixedDuration, used for billing estimation.
func veoRequestNeedsFixedDuration(req relaycommon.TaskSubmitReq) bool {
	dc := relaycommon.DCMediaMetadata{}
	if err := req.UnmarshalMetadata(&dc); err != nil {
		return false
	}
	return strings.TrimSpace(dc.LastFrameImage) != "" || len(nonEmpty(dc.ReferenceImages)) > 0
}

// EffectiveVeoDuration is the duration billed and sent upstream.
func EffectiveVeoDuration(req relaycommon.TaskSubmitReq) int {
	if veoRequestNeedsFixedDuration(req) {
		return VeoFixedDurationSeconds
	}
	return ResolveVeoDuration(req.Metadata, req.Duration, req.Seconds)
}

// BuildVeoRequestPayload builds the predictLongRunning body shared by the
// Gemini API and Vertex AI Veo adaptors.
func BuildVeoRequestPayload(c *gin.Context, info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (*VeoRequestPayload, error) {
	var media *VeoMediaInputs
	if c != nil {
		if v, ok := c.Get(veoMediaContextKey); ok {
			media, _ = v.(*VeoMediaInputs)
		}
	}
	if media == nil {
		var err error
		if media, err = ResolveVeoMedia(c, info, req); err != nil {
			return nil, err
		}
	}

	instance := VeoInstance{
		Prompt:          req.Prompt,
		Image:           media.Image,
		LastFrame:       media.LastFrame,
		ReferenceImages: media.ReferenceImages,
	}

	params := &VeoParameters{}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, params); err != nil {
		return nil, fmt.Errorf("unmarshal metadata failed: %w", err)
	}
	if params.DurationSeconds == 0 && req.Duration > 0 {
		params.DurationSeconds = req.Duration
	}
	if media.RequiresFixedDuration() {
		if params.DurationSeconds != 0 && params.DurationSeconds != VeoFixedDurationSeconds && c != nil {
			logger.LogInfo(c, fmt.Sprintf("veo: durationSeconds %d overridden to %d (reference images / last frame require 8s)", params.DurationSeconds, VeoFixedDurationSeconds))
		}
		params.DurationSeconds = VeoFixedDurationSeconds
	}
	if params.Resolution == "" && req.Size != "" {
		params.Resolution = SizeToVeoResolution(req.Size)
	}
	if params.AspectRatio == "" && req.Size != "" {
		params.AspectRatio = SizeToVeoAspectRatio(req.Size)
	}
	params.Resolution = strings.ToLower(params.Resolution)
	params.SampleCount = 1

	return &VeoRequestPayload{
		Instances:  []VeoInstance{instance},
		Parameters: params,
	}, nil
}

// MarshalVeoRequestPayload is BuildVeoRequestPayload + JSON encoding.
func MarshalVeoRequestPayload(c *gin.Context, info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) ([]byte, error) {
	body, err := BuildVeoRequestPayload(c, info, req)
	if err != nil {
		return nil, err
	}
	return common.Marshal(body)
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
