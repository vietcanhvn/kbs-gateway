package constant

import "time"

type TaskPlatform string

const (
	// GeminiOmniUpstreamTimeout: thời gian tối đa cổng chờ lời gọi chặn
	// POST /v1beta/interactions của Google (thường 20–60 s).
	GeminiOmniUpstreamTimeout = 15 * time.Minute
	// GeminiOmniStaleAfter: interaction gemini-omni vẫn đang chạy sau mốc này
	// nghĩa là cổng đã mất lời gọi (khởi động lại giữa chừng) → bộ poll đánh
	// thất bại và hoàn tiền. Phải lớn hơn GeminiOmniUpstreamTimeout.
	GeminiOmniStaleAfter = GeminiOmniUpstreamTimeout + 5*time.Minute
	// GeminiOmniTaskIDPrefix: tiền tố id interaction do cổng cấp ("gw_…");
	// id cũ (trước khi đổi thiết kế) là id interaction của Google.
	GeminiOmniTaskIDPrefix = "gw_"
)

const (
	TaskPlatformSuno       TaskPlatform = "suno"
	TaskPlatformMidjourney              = "mj"
	// TaskPlatformGeminiOmni: interaction Gemini Omni Flash (POST /v1beta/interactions),
	// lưu để dính kênh/key, kiểm tra chủ sở hữu tệp và quyết toán tác vụ nền.
	TaskPlatformGeminiOmni TaskPlatform = "gemini-omni"
)

const (
	SunoActionMusic  = "MUSIC"
	SunoActionLyrics = "LYRICS"

	TaskActionGenerate          = "generate"
	TaskActionTextGenerate      = "textGenerate"
	TaskActionFirstTailGenerate = "firstTailGenerate"
	TaskActionReferenceGenerate = "referenceGenerate"
	TaskActionRemix             = "remixGenerate"
)

var SunoModel2Action = map[string]string{
	"suno_music":  SunoActionMusic,
	"suno_lyrics": SunoActionLyrics,
}
