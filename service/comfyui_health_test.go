package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
)

type comfyUIStatusChange struct {
	ID     int
	Status int
	Reason string
}

func runComfyUIHealthForTest(state *comfyUIHealthState, channels []comfyUIHealthChannel, down map[int]bool) []comfyUIStatusChange {
	var changes []comfyUIStatusChange
	runComfyUIHealthCheckOnce(state, channels,
		func(channel comfyUIHealthChannel) error {
			if down[channel.ID] {
				return errors.New("status 404")
			}
			return nil
		},
		func(channel comfyUIHealthChannel, status int, reason string) {
			changes = append(changes, comfyUIStatusChange{channel.ID, status, reason})
		})
	return changes
}

func TestComfyUIHealthDisablesAfterTwoConsecutiveFailuresOnly(t *testing.T) {
	state := newComfyUIHealthState()
	channel := comfyUIHealthChannel{ID: 4, Status: common.ChannelStatusEnabled, BaseURL: "https://pod.example", AutoBan: true}

	// Một lần không trả lời (mạng chập chờn) chưa tắt kênh.
	assert.Empty(t, runComfyUIHealthForTest(state, []comfyUIHealthChannel{channel}, map[int]bool{4: true}))
	// Trả lời lại thì đếm lại từ đầu.
	assert.Empty(t, runComfyUIHealthForTest(state, []comfyUIHealthChannel{channel}, nil))
	assert.Empty(t, runComfyUIHealthForTest(state, []comfyUIHealthChannel{channel}, map[int]bool{4: true}))
	// Hai lần liên tiếp: tắt, ghi rõ lý do của bước kiểm tra này.
	assert.Equal(t,
		[]comfyUIStatusChange{{4, common.ChannelStatusAutoDisabled, comfyUIHealthReason}},
		runComfyUIHealthForTest(state, []comfyUIHealthChannel{channel}, map[int]bool{4: true}))
}

func TestComfyUIHealthReenablesOnlyChannelsItDisabled(t *testing.T) {
	channels := []comfyUIHealthChannel{
		{ID: 1, Status: common.ChannelStatusAutoDisabled, BaseURL: "https://a.example", AutoBan: true, StatusReason: comfyUIHealthReason},
		// Tự tắt vì lý do khác (lỗi từ nhà cung cấp): không tự bật lại.
		{ID: 2, Status: common.ChannelStatusAutoDisabled, BaseURL: "https://b.example", AutoBan: true, StatusReason: "401 unauthorized"},
		// Quản trị tắt tay: không đụng.
		{ID: 3, Status: common.ChannelStatusManuallyDisabled, BaseURL: "https://c.example", AutoBan: true},
	}

	// Máy chủ vẫn tắt: không đổi gì.
	assert.Empty(t, runComfyUIHealthForTest(newComfyUIHealthState(), channels, map[int]bool{1: true, 2: true, 3: true}))
	// Máy chủ sống lại: chỉ kênh do bước kiểm tra này tắt được bật lên.
	assert.Equal(t,
		[]comfyUIStatusChange{{1, common.ChannelStatusEnabled, ""}},
		runComfyUIHealthForTest(newComfyUIHealthState(), channels, nil))
}

func TestComfyUIHealthSkipsChannelsWithoutAutoBanOrBaseURL(t *testing.T) {
	state := newComfyUIHealthState()
	channels := []comfyUIHealthChannel{
		{ID: 5, Status: common.ChannelStatusEnabled, BaseURL: "https://pod.example", AutoBan: false},
		{ID: 6, Status: common.ChannelStatusEnabled, BaseURL: "  ", AutoBan: true},
	}
	down := map[int]bool{5: true, 6: true}
	assert.Empty(t, runComfyUIHealthForTest(state, channels, down))
	assert.Empty(t, runComfyUIHealthForTest(state, channels, down))
	assert.Empty(t, runComfyUIHealthForTest(state, channels, down))
}
