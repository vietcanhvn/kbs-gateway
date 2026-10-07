package constant

type TaskPlatform string

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
