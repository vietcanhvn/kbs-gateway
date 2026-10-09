package mediaworkflow

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Timeline workflows (ComfyUI-Easy-Media "MultiTrack Editor" + MiniMax H3
// "MultiTrack Project"): one long video made of segments, each continuing the
// previous one. The whole editor state is one JSON string widget
// (track_data), so a request is turned into that JSON here and sent as a
// single node override. Only the task / video / audio tracks the gateway
// needs are written; the stored workflow's track_data is the template for
// everything else (frame rate, track styling).

// RoleTimeline binds the track_data widget of a MultiTrack Editor node.
const RoleTimeline = "timeline"

// TimelineSegmentSpec is one segment as a client asks for it (DC-Media
// metadata.segments). Images / Video index into the request's media.
type TimelineSegmentSpec struct {
	Prompt     string  `json:"prompt"`
	Seconds    float64 `json:"seconds"`
	Mode       string  `json:"mode,omitempty"`       // r2v | t2v | i2v | fl2v | v2v | rv2v | l2v
	Continuity string  `json:"continuity,omitempty"` // shot | context | context_drift
	Images     []int   `json:"images,omitempty"`
	Video      *int    `json:"video,omitempty"`
}

// TimelineSegment is a resolved segment.
type TimelineSegment struct {
	Prompt     string
	Seconds    float64
	Mode       string
	Continuity string
	Images     []int
	Video      int // -1 = none
}

const (
	timelineMinSeconds     = 1
	timelineMaxSeconds     = 20
	timelineDefaultSeconds = 8
	timelineMaxSegments    = 24
)

var timelineModes = map[string]bool{"r2v": true, "t2v": true, "i2v": true, "ti2v": true, "fl2v": true, "v2v": true, "rv2v": true, "l2v": true}
var timelineContinuity = map[string]bool{"shot": true, "context": true, "context_drift": true}

// "[Đoạn 2 – 8s – TI2V – context]" / "[Segment 2, 8s, r2v, shot]" at the start of a line.
var timelineMarker = regexp.MustCompile(`(?im)^\s*\[(?:đoạn|doan|segment|seg|shot)\s*\d*([^\]]*)\]\s*`)
var timelineSecondsToken = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(?:s|giây|giay|sec|seconds?)\b`)

// PlanTimeline resolves the segments of a request: explicit specs win, then
// "[Đoạn n – 8s – mode – continuity]" markers in the prompt, then one segment
// with the whole prompt. imageCount / videoCount are the request's media.
func PlanTimeline(prompt string, specs []TimelineSegmentSpec, fallbackSeconds float64, imageCount, videoCount int) ([]TimelineSegment, error) {
	if len(specs) == 0 {
		specs = parseTimelineMarkers(prompt)
	}
	if len(specs) == 0 {
		seconds := fallbackSeconds
		if seconds <= 0 {
			seconds = timelineDefaultSeconds
		}
		specs = []TimelineSegmentSpec{{Prompt: prompt, Seconds: seconds}}
	}
	if len(specs) > timelineMaxSegments {
		return nil, requestErrorf("at most %d timeline segments, got %d", timelineMaxSegments, len(specs))
	}
	allImages := make([]int, imageCount)
	for i := range allImages {
		allImages[i] = i
	}
	out := make([]TimelineSegment, 0, len(specs))
	for index, spec := range specs {
		if strings.TrimSpace(spec.Prompt) == "" {
			return nil, requestErrorf("timeline segment %d has no prompt", index+1)
		}
		seconds := spec.Seconds
		if seconds <= 0 {
			seconds = timelineDefaultSeconds
		}
		seconds = math.Max(timelineMinSeconds, math.Min(timelineMaxSeconds, seconds))
		images := spec.Images
		if images == nil {
			images = allImages
		}
		for _, image := range images {
			if image < 0 || image >= imageCount {
				return nil, requestErrorf("timeline segment %d uses image %d, but only %d image(s) were sent", index+1, image+1, imageCount)
			}
		}
		video := -1
		if spec.Video != nil {
			if *spec.Video < 0 || *spec.Video >= videoCount {
				return nil, requestErrorf("timeline segment %d uses video %d, but only %d video(s) were sent", index+1, *spec.Video+1, videoCount)
			}
			video = *spec.Video
		}
		mode := strings.ToLower(strings.TrimSpace(spec.Mode))
		if mode == "ti2v" {
			mode = "i2v"
		}
		if mode == "" {
			switch {
			case video >= 0:
				mode = "rv2v"
			case len(images) > 0:
				mode = "r2v"
			default:
				mode = "t2v"
			}
		}
		if !timelineModes[mode] {
			return nil, requestErrorf("timeline segment %d has unknown mode %q", index+1, spec.Mode)
		}
		if (mode == "v2v" || mode == "rv2v") && video < 0 {
			if videoCount == 0 {
				return nil, requestErrorf("timeline segment %d (%s) needs a video", index+1, strings.ToUpper(mode))
			}
			video = 0
		}
		switch mode {
		case "t2v", "l2v":
			images = nil
		case "i2v":
			if len(images) == 0 {
				return nil, requestErrorf("timeline segment %d (I2V) needs an image", index+1)
			}
			images = images[:1]
		case "fl2v":
			if len(images) < 2 {
				return nil, requestErrorf("timeline segment %d (FL2V) needs two images", index+1)
			}
			images = images[:2]
		}
		continuity := strings.ToLower(strings.TrimSpace(spec.Continuity))
		if continuity == "" {
			continuity = "context"
			if index == 0 {
				continuity = "shot"
			}
		}
		if index == 0 {
			continuity = "shot"
		}
		if !timelineContinuity[continuity] {
			return nil, requestErrorf("timeline segment %d has unknown continuity %q", index+1, spec.Continuity)
		}
		out = append(out, TimelineSegment{Prompt: spec.Prompt, Seconds: seconds, Mode: mode, Continuity: continuity, Images: images, Video: video})
	}
	return out, nil
}

// TimelineSeconds is the length of the finished video (what is billed).
func TimelineSeconds(segments []TimelineSegment, frameRate float64) float64 {
	if frameRate <= 0 {
		frameRate = 24
	}
	frames := 0
	for _, segment := range segments {
		frames += int(math.Round(segment.Seconds * frameRate))
	}
	return float64(frames) / frameRate
}

func parseTimelineMarkers(prompt string) []TimelineSegmentSpec {
	matches := timelineMarker.FindAllStringSubmatchIndex(prompt, -1)
	if len(matches) == 0 {
		return nil
	}
	specs := make([]TimelineSegmentSpec, 0, len(matches))
	for i, match := range matches {
		bodyEnd := len(prompt)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		spec := TimelineSegmentSpec{Prompt: strings.TrimSpace(prompt[match[1]:bodyEnd])}
		for _, token := range strings.FieldsFunc(prompt[match[2]:match[3]], func(r rune) bool {
			return r == '–' || r == '-' || r == '—' || r == ',' || r == '|' || r == ';'
		}) {
			token = strings.ToLower(strings.TrimSpace(token))
			if seconds := timelineSecondsToken.FindStringSubmatch(token); seconds != nil {
				value, _ := strconv.ParseFloat(strings.ReplaceAll(seconds[1], ",", "."), 64)
				spec.Seconds = value
				continue
			}
			if timelineModes[token] {
				spec.Mode = token
				continue
			}
			if timelineContinuity[strings.ReplaceAll(token, " ", "_")] {
				spec.Continuity = strings.ReplaceAll(token, " ", "_")
			}
		}
		specs = append(specs, spec)
	}
	return specs
}

// TimelineMedia are the files already uploaded for the run (names the
// executor's LoadImage-style inputs accept).
type TimelineMedia struct {
	Images []string
	Videos []string
	Audios []string
	// AudioLock: the first audio is the soundtrack of the whole video (the
	// characters lip-sync to it). Otherwise it is a voice reference.
	AudioLock bool
}

// BuildTrackData writes the segments into the stored track_data template.
// renderPrompt applies the workflow's prompt template (tag rewriting).
func BuildTrackData(template string, segments []TimelineSegment, media TimelineMedia, renderPrompt func(string) string) (string, error) {
	var data map[string]any
	if err := common.UnmarshalJsonStr(template, &data); err != nil {
		return "", fmt.Errorf("timeline template is not valid JSON: %w", err)
	}
	frameRate := 24.0
	if value, ok := data["frame_rate"].(float64); ok && value > 0 {
		frameRate = value
	}
	tracks, _ := data["tracks"].([]any)
	taskTrack := findTrack(tracks, "task")
	if taskTrack == nil {
		taskTrack = map[string]any{"id": newTimelineID(), "name": "Task 0", "type": "task", "task_mode": "default", "color": "var(--multitrack-task-bg)", "muted": false, "solo": false, "volume_db": 0, "locked": false}
	}
	segmentColor, _ := firstSegmentField(taskTrack, "color").(string)
	if segmentColor == "" {
		segmentColor = "var(--multitrack-task-bg)"
	}

	taskSegments := make([]any, 0, len(segments))
	videoSegments := make([]any, 0)
	frame := 0
	for _, segment := range segments {
		length := int(math.Round(segment.Seconds * frameRate))
		start, end := frame, frame+length
		frame = end
		taskMode, refSize := "default", "match"
		switch segment.Mode {
		case "r2v", "rv2v":
			taskMode, refSize = "ref", "max"
		case "v2v":
			taskMode = "edit"
		case "l2v":
			taskMode = "l2v"
		}
		images := make([]any, 0, len(segment.Images))
		for _, index := range segment.Images {
			name := media.Images[index]
			images = append(images, map[string]any{"id": newTimelineID(), "source_type": "input", "file_path": name, "file_name": name})
		}
		prompt := segment.Prompt
		if renderPrompt != nil {
			prompt = renderPrompt(prompt)
		}
		taskSegments = append(taskSegments, map[string]any{
			"id": newTimelineID(), "start_frame": start, "end_frame": end, "color": segmentColor,
			"content": map[string]any{
				"media_type": "none", "task_mode": taskMode, "continuity_mode": segment.Continuity,
				"ref_image_size": refSize, "images": images, "muted": false, "volume_db": 0, "user_prompt": prompt,
			},
		})
		if segment.Video >= 0 {
			name := media.Videos[segment.Video]
			videoSegments = append(videoSegments, map[string]any{
				"id": newTimelineID(), "start_frame": start, "end_frame": end, "color": "var(--primary)",
				"content": map[string]any{"media_type": "video", "source_type": "input", "file_path": name, "file_name": name, "muted": true, "volume_db": 0},
			})
		}
	}
	taskTrack["segments"] = taskSegments

	nextTracks := []any{taskTrack}
	if videoTrack := findTrack(tracks, "video"); videoTrack != nil || len(videoSegments) > 0 {
		if videoTrack == nil {
			videoTrack = map[string]any{"id": newTimelineID(), "name": "Video 0", "type": "video", "color": "var(--primary)", "muted": false, "solo": false, "volume_db": 0, "locked": false}
		}
		videoTrack["segments"] = videoSegments
		nextTracks = append(nextTracks, videoTrack)
	}
	if len(media.Audios) > 0 {
		name := media.Audios[0]
		content := map[string]any{"media_type": "audio", "source_type": "input", "file_path": name, "file_name": name, "muted": false, "volume_db": 0}
		if !media.AudioLock {
			content["shared_reference"] = true
		}
		nextTracks = append(nextTracks, map[string]any{
			"id": newTimelineID(), "name": "Audio 0", "type": "audio", "color": "var(--primary)", "muted": false, "solo": false,
			"volume_db": 0, "locked": false, "audio_locked": media.AudioLock,
			"segments": []any{map[string]any{"id": newTimelineID(), "start_frame": 0, "end_frame": frame, "color": "var(--primary)", "content": content}},
		})
	}
	data["tracks"] = nextTracks
	data["total_length"] = frame
	data["frame_rate"] = frameRate
	out, err := common.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// TimelineFrameRate reads the template's frame rate (24 when missing).
func TimelineFrameRate(template string) float64 {
	var data struct {
		FrameRate float64 `json:"frame_rate"`
	}
	if common.UnmarshalJsonStr(template, &data) == nil && data.FrameRate > 0 {
		return data.FrameRate
	}
	return 24
}

func findTrack(tracks []any, kind string) map[string]any {
	for _, item := range tracks {
		if track, ok := item.(map[string]any); ok && track["type"] == kind {
			copied := make(map[string]any, len(track))
			for key, value := range track {
				copied[key] = value
			}
			return copied
		}
	}
	return nil
}

func firstSegmentField(track map[string]any, field string) any {
	segments, _ := track["segments"].([]any)
	if len(segments) == 0 {
		return nil
	}
	segment, _ := segments[0].(map[string]any)
	return segment[field]
}

func newTimelineID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(int64(len(raw)), 10)
	}
	text := hex.EncodeToString(raw[:])
	return text[0:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:32]
}
