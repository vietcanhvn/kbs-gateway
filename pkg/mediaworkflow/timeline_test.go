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
	segments, err := PlanTimeline(prompt, nil, 0, 2, 0)
	require.NoError(t, err)
	require.Len(t, segments, 3)
	assert.Equal(t, TimelineSegment{Prompt: "She walks in.", Seconds: 8, Mode: "r2v", Continuity: "shot", Images: []int{0, 1}, Video: -1}, segments[0])
	assert.Equal(t, TimelineSegment{Prompt: "She climbs.", Seconds: 6, Mode: "i2v", Continuity: "context", Images: []int{0}, Video: -1}, segments[1])
	assert.Equal(t, TimelineSegment{Prompt: "Wide view.", Seconds: 5, Mode: "t2v", Continuity: "shot", Images: nil, Video: -1}, segments[2])
	assert.InDelta(t, 19.0, TimelineSeconds(segments, 24), 0.01)
}

func TestPlanTimelineSpecsAndErrors(t *testing.T) {
	video := 0
	segments, err := PlanTimeline("ignored", []TimelineSegmentSpec{
		{Prompt: "a", Seconds: 8},
		{Prompt: "b", Seconds: 40, Continuity: "shot"},
		{Prompt: "c", Mode: "v2v", Video: &video},
	}, 0, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, "t2v", segments[0].Mode, "no image: text to video")
	assert.Equal(t, 20.0, segments[1].Seconds, "clamped to the segment maximum")
	assert.Equal(t, "shot", segments[1].Continuity)
	assert.Equal(t, 0, segments[2].Video)

	_, err = PlanTimeline("", []TimelineSegmentSpec{{Prompt: "a", Mode: "fl2v"}}, 0, 1, 0)
	assert.ErrorContains(t, err, "needs two images")
	_, err = PlanTimeline("", []TimelineSegmentSpec{{Prompt: "a", Images: []int{3}}}, 0, 1, 0)
	assert.ErrorContains(t, err, "only 1 image")
	_, err = PlanTimeline("", []TimelineSegmentSpec{{Prompt: " "}}, 0, 0, 0)
	assert.ErrorContains(t, err, "no prompt")

	single, err := PlanTimeline("one take", nil, 10, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, []TimelineSegment{{Prompt: "one take", Seconds: 10, Mode: "t2v", Continuity: "shot", Video: -1}}, single)
}

func TestBuildTrackData(t *testing.T) {
	segments := []TimelineSegment{
		{Prompt: "@image1 walks", Seconds: 8, Mode: "r2v", Continuity: "shot", Images: []int{0}, Video: -1},
		{Prompt: "continues", Seconds: 4, Mode: "v2v", Continuity: "context", Video: 0},
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
	out, err := BuildTrackData(timelineTemplate, []TimelineSegment{{Prompt: "x", Seconds: 5, Mode: "t2v", Continuity: "shot", Video: -1}},
		TimelineMedia{Audios: []string{"api/voice.wav"}}, nil)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &data))
	tracks := data["tracks"].([]any)
	audio := tracks[len(tracks)-1].(map[string]any)
	assert.Equal(t, false, audio["audio_locked"])
	assert.Equal(t, true, audio["segments"].([]any)[0].(map[string]any)["content"].(map[string]any)["shared_reference"])
}
