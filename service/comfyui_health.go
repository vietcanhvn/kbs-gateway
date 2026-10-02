package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// Tự kiểm tra máy chủ ComfyUI của các kênh ComfyUI.
//
// Kênh ComfyUI trỏ tới một máy chủ GPU tự dựng (pod thuê theo giờ), hay được tắt
// ngoài giờ làm việc. Gateway không biết máy chủ đã tắt cho tới khi có người gọi
// và bị lỗi, nên mô hình vẫn hiện "đang phục vụ". Ở đây cứ vài phút hỏi máy chủ
// một lần (GET /queue, không tốn tiền): không trả lời hai lần liên tiếp thì tắt
// kênh ở trạng thái "tự tắt" - mô hình của kênh biến khỏi danh sách đang phục vụ
// và ứng dụng làm mờ nó; máy chủ trả lời lại thì bật kênh lên.
//
// Chỉ đụng tới kênh bật "tự động tắt" (auto_ban), và chỉ tự bật lại những kênh do
// chính bước kiểm tra này tắt - kênh quản trị tắt tay hay bị tắt vì lý do khác
// thì để nguyên. Các kênh trả tiền theo lượt gọi (FAL, BytePlus...) không nằm
// trong phạm vi: gọi thử chúng là tốn tiền.

const (
	comfyUIHealthInterval          = 2 * time.Minute
	comfyUIHealthTimeout           = 10 * time.Second
	comfyUIHealthFailuresToDisable = 2
	// Lý do ghi vào kênh khi bước kiểm tra này tắt nó; tiền tố dùng để nhận ra.
	comfyUIHealthReasonPrefix = "comfyui_health:"
	comfyUIHealthReason       = comfyUIHealthReasonPrefix + " máy chủ ComfyUI không trả lời"
)

type comfyUIHealthChannel struct {
	ID           int
	Name         string
	Status       int
	BaseURL      string
	Proxy        string
	AutoBan      bool
	StatusReason string
}

// comfyUIHealthState đếm số lần liên tiếp không trả lời của từng kênh.
type comfyUIHealthState struct {
	failures map[int]int
}

func newComfyUIHealthState() *comfyUIHealthState {
	return &comfyUIHealthState{failures: map[int]int{}}
}

// runComfyUIHealthCheckOnce xét từng kênh một lượt. probe trả nil khi máy chủ
// còn sống; setStatus đổi trạng thái kênh.
func runComfyUIHealthCheckOnce(
	state *comfyUIHealthState,
	channels []comfyUIHealthChannel,
	probe func(channel comfyUIHealthChannel) error,
	setStatus func(channel comfyUIHealthChannel, status int, reason string),
) {
	for _, channel := range channels {
		if !channel.AutoBan || strings.TrimSpace(channel.BaseURL) == "" {
			continue
		}
		disabledByHealth := channel.Status == common.ChannelStatusAutoDisabled &&
			strings.HasPrefix(channel.StatusReason, comfyUIHealthReasonPrefix)
		if channel.Status != common.ChannelStatusEnabled && !disabledByHealth {
			continue
		}

		err := probe(channel)
		if err == nil {
			delete(state.failures, channel.ID)
			if disabledByHealth {
				setStatus(channel, common.ChannelStatusEnabled, "")
			}
			continue
		}
		if disabledByHealth {
			continue
		}
		state.failures[channel.ID]++
		if state.failures[channel.ID] >= comfyUIHealthFailuresToDisable {
			delete(state.failures, channel.ID)
			setStatus(channel, common.ChannelStatusAutoDisabled, comfyUIHealthReason)
		}
	}
}

// probeComfyUIServer hỏi GET <base>/queue. Máy chủ trả lời (kể cả đòi đăng nhập)
// là còn sống; lỗi mạng, 404 (proxy báo máy đã tắt) hay 5xx là không.
func probeComfyUIServer(channel comfyUIHealthChannel) error {
	client, err := GetHttpClientWithProxy(channel.Proxy)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), comfyUIHealthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(channel.BaseURL, "/")+"/queue", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func loadComfyUIHealthChannels() ([]comfyUIHealthChannel, error) {
	var rows []*model.Channel
	err := model.DB.Where("type = ? AND status IN ?", constant.ChannelTypeComfyUI,
		[]int{common.ChannelStatusEnabled, common.ChannelStatusAutoDisabled}).Omit("key").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	channels := make([]comfyUIHealthChannel, 0, len(rows))
	for _, row := range rows {
		reason, _ := row.GetOtherInfo()["status_reason"].(string)
		channels = append(channels, comfyUIHealthChannel{
			ID:           row.Id,
			Name:         row.Name,
			Status:       row.Status,
			BaseURL:      row.GetBaseURL(),
			Proxy:        row.GetSetting().Proxy,
			AutoBan:      row.GetAutoBan(),
			StatusReason: reason,
		})
	}
	return channels, nil
}

var comfyUIHealthOnce sync.Once

// StartComfyUIHealthCheckTask chạy bước kiểm tra định kỳ trên nút chính.
func StartComfyUIHealthCheckTask() {
	comfyUIHealthOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			state := newComfyUIHealthState()
			ticker := time.NewTicker(comfyUIHealthInterval)
			defer ticker.Stop()
			for range ticker.C {
				channels, err := loadComfyUIHealthChannels()
				if err != nil {
					common.SysLog("comfyui health check: failed to load channels: " + err.Error())
					continue
				}
				runComfyUIHealthCheckOnce(state, channels, probeComfyUIServer, func(channel comfyUIHealthChannel, status int, reason string) {
					if model.UpdateChannelStatus(channel.ID, "", status, reason) {
						common.SysLog(fmt.Sprintf("comfyui health check: channel «%s» (#%d) -> status %d %s", channel.Name, channel.ID, status, reason))
					}
				})
			}
		})
	})
}
