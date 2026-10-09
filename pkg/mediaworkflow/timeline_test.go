package mediaworkflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const timelineTemplate = `{"muted":false,"volume_db":0,"task_markers":[],"task_overview":false,"total_length":192,"frame_rate":24,
"tracks":[{"id":"t","name":"Task 0","type":"task","task_mode":"default","color":"c","muted":false,"solo":false,"volume_db":0,"locked":false,
"segments":[{"id":"s","start_frame":0,"end_frame":192,"color":"seg-color","content":{"media_type":"none","task_mode":"ref","images":[]}}]},
{"id":"v","name":"Video 0","type":"video","color":"var(--primary)","muted":false,"segments":[]}]}`

func TestPlanTimelineFromMarkers(t *testing.T) {
	prompt := "[Đoạn 1 – 8s] She walks in.\n[Đoạn 2 – 6s – TI2V – context] She climbs.\n[Segment 3, 5s, t2v, shot] Wide view."
	segments, err := PlanTimeline(prompt, nil, 0, 2, 0, 0)
	require.NoError(t, err)
	require.Len(t, segments, 3)
	assert.Equal(t, TimelineSegment{Prompt: "She walks in.", Seconds: 8, Mode: "r2v", Continuity: "shot", Images: []int{0, 1}}, segments[0])
	assert.Equal(t, TimelineSegment{Prompt: "She climbs.", Seconds: 6, Mode: "ti2v", Continuity: "context", Images: []int{0, 1}}, segments[1], "TI2V keeps every image (FL2V with two)")
	assert.Equal(t, TimelineSegment{Prompt: "Wide view.", Seconds: 5, Mode: "t2v", Continuity: "shot", Images: nil}, segments[2])
	assert.InDelta(t, 19.0, TimelineSeconds(segments, 24), 0.01)
}

func TestPlanTimelineSpecsAndErrors(t *testing.T) {
	video := 0
	segments, err := PlanTimeline("ignored", []TimelineSegmentSpec{
		{Prompt: "a", Seconds: 8},
		{Prompt: "b", Seconds: 40, Continuity: "shot"},
		{Prompt: "c", Mode: "v2v", Video: &video},
	}, 0, 0, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, "t2v", segments[0].Mode, "no image: text to video")
	assert.Equal(t, 20.0, segments[1].Seconds, "clamped to the segment maximum")
	assert.Equal(t, "shot", segments[1].Continuity)
	assert.Equal(t, []int{0}, segments[2].Videos)

	_, err = PlanTimeline("", []TimelineSegmentSpec{{Prompt: "a", Mode: "fl2v"}}, 0, 1, 0, 0)
	assert.ErrorContains(t, err, "needs two images")
	_, err = PlanTimeline("", []TimelineSegmentSpec{{Prompt: "a", Images: []int{3}}}, 0, 1, 0, 0)
	assert.ErrorContains(t, err, "only 1 image")
	_, err = PlanTimeline("", []TimelineSegmentSpec{{Prompt: " "}}, 0, 0, 0, 0)
	assert.ErrorContains(t, err, "no prompt")

	single, err := PlanTimeline("one take", nil, 10, 0, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, []TimelineSegment{{Prompt: "one take", Seconds: 10, Mode: "t2v", Continuity: "shot"}}, single)
}

func TestBuildTrackData(t *testing.T) {
	segments := []TimelineSegment{
		{Prompt: "@image1 walks", Seconds: 8, Mode: "r2v", Continuity: "shot", Images: []int{0}},
		{Prompt: "continues", Seconds: 4, Mode: "v2v", Continuity: "context", Videos: []int{0}},
	}
	out, err := BuildTrackData(timelineTemplate, segments, TimelineMedia{
		Images: []string{"api/a.png"}, Videos: []string{"api/v.mp4"}, Audios: []string{"api/song.mp3"}, AudioLock: true,
	}, PromptTemplate{ReferenceTags: map[string]string{"image": "<Picture {n}>"}}.Render)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &data))
	assert.EqualValues(t, 288, data["total_length"])
	tracks := data["tracks"].([]any)
	require.Len(t, tracks, 3)

	task := tracks[0].(map[string]any)
	taskSegments := task["segments"].([]any)
	first := taskSegments[0].(map[string]any)
	assert.EqualValues(t, 0, first["start_frame"])
	assert.EqualValues(t, 192, first["end_frame"])
	assert.Equal(t, "seg-color", first["color"])
	content := first["content"].(map[string]any)
	assert.Equal(t, "ref", content["task_mode"])
	assert.Equal(t, "max", content["ref_image_size"])
	assert.Equal(t, "shot", content["continuity_mode"])
	assert.Equal(t, "<Picture 1> walks", content["user_prompt"])
	assert.Equal(t, "api/a.png", content["images"].([]any)[0].(map[string]any)["file_path"])
	second := taskSegments[1].(map[string]any)["content"].(map[string]any)
	assert.Equal(t, "edit", second["task_mode"])
	assert.Equal(t, "context", second["continuity_mode"])

	videoSegments := tracks[1].(map[string]any)["segments"].([]any)
	require.Len(t, videoSegments, 1)
	assert.EqualValues(t, 192, videoSegments[0].(map[string]any)["start_frame"])
	assert.Equal(t, "api/v.mp4", videoSegments[0].(map[string]any)["content"].(map[string]any)["file_path"])

	audio := tracks[2].(map[string]any)
	assert.Equal(t, "audio", audio["type"])
	assert.Equal(t, true, audio["audio_locked"])
	audioSegment := audio["segments"].([]any)[0].(map[string]any)
	assert.EqualValues(t, 288, audioSegment["end_frame"])
	assert.Nil(t, audioSegment["content"].(map[string]any)["shared_reference"])
}

func TestBuildTrackDataVoiceReference(t *testing.T) {
	out, err := BuildTrackData(timelineTemplate, []TimelineSegment{{Prompt: "x", Seconds: 5, Mode: "t2v", Continuity: "shot"}},
		TimelineMedia{Audios: []string{"api/voice.wav"}}, nil)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &data))
	tracks := data["tracks"].([]any)
	audio := tracks[len(tracks)-1].(map[string]any)
	assert.Equal(t, false, audio["audio_locked"])
	assert.Equal(t, true, audio["segments"].([]any)[0].(map[string]any)["content"].(map[string]any)["shared_reference"])
}

func TestPlanTimelineModesLikeEasyMedia(t *testing.T) {
	segments, err := PlanTimeline("", []TimelineSegmentSpec{
		{Prompt: "a", Mode: "vi2v", Images: []int{1}, Videos: []int{0, 1}},
		{Prompt: "b", Mode: "r2v", Images: []int{0}, Videos: []int{1}, Audios: []int{0}},
		{Prompt: "c", Mode: "l2v", Images: []int{1}},
		{Prompt: "d", Mode: "auto", Images: []int{}},
	}, 0, 2, 2, 1)
	require.NoError(t, err)
	assert.Equal(t, TimelineSegment{Prompt: "a", Seconds: 8, Mode: "v2v", Continuity: "shot", Images: []int{1}, Videos: []int{0, 1}}, segments[0], "VI2V = edit with images")
	assert.Equal(t, "rv2v", segments[1].Mode, "reference with a video")
	assert.Equal(t, []int{0}, segments[1].Audios)
	assert.Equal(t, []int{1}, segments[2].Images, "L2V keeps its last-frame image")
	assert.Equal(t, "t2v", segments[3].Mode, "no media chosen")

	_, err = PlanTimeline("", []TimelineSegmentSpec{{Prompt: "a", Mode: "l2v", Images: []int{}}}, 0, 1, 0, 0)
	assert.ErrorContains(t, err, "last frame")
	_, err = PlanTimeline("", []TimelineSegmentSpec{{Prompt: "a", Audios: []int{0, 0, 0, 0}}}, 0, 0, 0, 1)
	assert.ErrorContains(t, err, "at most 3")
}

func TestBuildTrackDataPerSegmentMedia(t *testing.T) {
	segments := []TimelineSegment{
		{Prompt: "@image2 dances to @audio2 like @video2", Seconds: 5, Mode: "v2v", Continuity: "shot", Images: []int{1}, Videos: []int{0, 1}, Audios: []int{1}},
		{Prompt: "@image1 waves", Seconds: 5, Mode: "r2v", Continuity: "context", Images: []int{0}},
	}
	out, err := BuildTrackData(timelineTemplate, segments, TimelineMedia{
		Images: []string{"a.png", "b.png"}, Videos: []string{"v1.mp4", "v2.mp4"}, Audios: []string{"s1.mp3", "s2.mp3"},
	}, PromptTemplate{ReferenceTags: map[string]string{"image": "<Picture {n}>", "video": "<Video {n}>", "audio": "<Audio {n}>"}}.Render)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &data))
	tracks := data["tracks"].([]any)
	require.Len(t, tracks, 4, "task + 2 video lanes + 1 audio lane; no whole-video audio when parts chose audio")
	first := tracks[0].(map[string]any)["segments"].([]any)[0].(map[string]any)["content"].(map[string]any)
	assert.Equal(t, "edit", first["task_mode"])
	assert.Len(t, first["images"], 1, "VI2V keeps its image")
	assert.Equal(t, "<Picture 1> dances to <Audio 1> like <Video 2>", first["user_prompt"], "tags count the part's own files")
	lane2 := tracks[2].(map[string]any)
	assert.Equal(t, "video", lane2["type"])
	assert.Equal(t, "v2.mp4", lane2["segments"].([]any)[0].(map[string]any)["content"].(map[string]any)["file_path"])
	audio := tracks[3].(map[string]any)
	assert.Equal(t, false, audio["audio_locked"])
	audioSegment := audio["segments"].([]any)[0].(map[string]any)
	assert.EqualValues(t, 120, audioSegment["end_frame"], "audio covers only its own part")
	assert.Equal(t, "s2.mp3", audioSegment["content"].(map[string]any)["file_path"])
}
