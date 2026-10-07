package controller

// Chuyển tiếp Gemini Omni Flash (Interactions API) nguyên dạng Google cho client
// chỉ có token cổng (KSB):
//
//	POST /v1beta/interactions            tạo video; cổng luôn gửi background=true nên
//	                                     trả ngay id + status in_progress (Cloudflare
//	                                     Tunnel cắt yêu cầu sau ~100 s)
//	GET  /v1beta/interactions/{id}       hỏi trạng thái; khi xong thì quyết toán (1 lần)
//	POST /v1beta/interactions/{id}:cancel huỷ; hoàn tiền trừ trước (1 lần)
//	GET  /v1beta/files/{id}              metadata tệp (state PROCESSING/ACTIVE/FAILED)
//	GET  /v1beta/files/{id}:download     tải video (truyền luồng)
//
// Mọi lời gọi sau POST dùng ĐÚNG kênh + key đã tạo interaction (lưu ở dòng
// tasks platform gemini-omni) và chỉ chủ sở hữu (cùng user) mới đọc được.
// Cách tính tiền: xem relay/channel/task/gemini/omni.go.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	taskgemini "github.com/QuantumNous/new-api/relay/channel/task/gemini"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

const (
	// omniCreateTimeout: tạo interaction nền thường trả trong vài giây.
	omniCreateTimeout = 90 * time.Second
	// omniMaxResponseBytes: chặn phản hồi base64 quá lớn.
	omniMaxResponseBytes = 512 << 20
	// omniFileLookupWindow: tệp Google tồn tại 48 giờ.
	omniFileLookupWindow = 48 * time.Hour
	// omniFileLookupLimit: số interaction gần nhất dò khi client không gửi interaction_id.
	omniFileLookupLimit = 500
)

// omniError trả lỗi theo dạng của Google để client dùng chung một cách đọc lỗi.
func omniError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": gin.H{
		"code":    status,
		"message": message,
		"status":  strings.ToUpper(strings.ReplaceAll(http.StatusText(status), " ", "_")),
	}})
}

// omniGatewayBase là gốc URL cổng dùng để viết lại URI tệp (giống BuildProxyURL của video).
func omniGatewayBase(c *gin.Context) string {
	if base := strings.TrimRight(system_setting.ServerAddress, "/"); base != "" {
		return base
	}
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

func omniChannelBaseURL(base string) string {
	if strings.TrimSpace(base) == "" {
		return constant.ChannelBaseURLs[constant.ChannelTypeGemini]
	}
	return strings.TrimRight(base, "/")
}

// RelayGeminiInteraction: POST /v1beta/interactions (sau TokenAuth + Distribute).
func RelayGeminiInteraction(c *gin.Context) {
	info, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		omniError(c, http.StatusInternalServerError, err.Error())
		return
	}
	info.InitChannelMeta(c)

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		omniError(c, http.StatusBadRequest, "read request body failed: "+err.Error())
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		omniError(c, http.StatusBadRequest, "read request body failed: "+err.Error())
		return
	}
	req, err := taskgemini.ParseOmniRequest(body)
	if err != nil {
		omniError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Lượt tiếp theo (edit/extend) phải về đúng kênh + key của interaction trước:
	// interaction của Google gắn với key/project đã tạo nó.
	if req.PreviousInteractionID != "" {
		prev, exists, err := model.GetByTaskId(info.UserId, req.PreviousInteractionID)
		if err != nil {
			omniError(c, http.StatusInternalServerError, "query previous interaction failed")
			return
		}
		if !exists || prev.Platform != constant.TaskPlatformGeminiOmni {
			omniError(c, http.StatusNotFound, "previous_interaction_id not found for this account")
			return
		}
		ch, err := model.GetChannelById(prev.ChannelId, true)
		if err != nil {
			omniError(c, http.StatusBadRequest, "channel of the previous interaction no longer exists")
			return
		}
		if ch.Status != common.ChannelStatusEnabled {
			omniError(c, http.StatusBadRequest, "channel of the previous interaction is disabled")
			return
		}
		if setupErr := middleware.SetupContextForSelectedChannel(c, ch, info.OriginModelName); setupErr != nil {
			omniError(c, http.StatusServiceUnavailable, setupErr.Error())
			return
		}
		if prev.PrivateData.Key != "" {
			common.SetContextKey(c, constant.ContextKeyChannelKey, prev.PrivateData.Key)
		}
		info.InitChannelMeta(c)
	}
	if info.ChannelType != constant.ChannelTypeGemini {
		omniError(c, http.StatusBadRequest, "the selected channel is not a Gemini API channel; Interactions API needs a Gemini channel")
		return
	}

	info.UpstreamModelName = info.OriginModelName
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		omniError(c, http.StatusBadRequest, "model mapping failed: "+err.Error())
		return
	}

	// Giá: ModelPrice = USD mỗi giây video 720p; trừ trước mức tối đa 10 s.
	priceData, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		omniError(c, http.StatusBadRequest, err.Error())
		return
	}
	if !priceData.UsePrice {
		omniError(c, http.StatusBadRequest, fmt.Sprintf("model %s has no per-second price (ModelPrice, USD per second at 720p) configured", info.OriginModelName))
		return
	}
	resolutionRatio, _ := taskgemini.OmniResolutionRatio(req.Resolution)
	groupRatio := priceData.GroupRatioInfo.GroupRatio
	preQuota, clamp := taskgemini.OmniQuota(priceData.ModelPrice, groupRatio, taskgemini.OmniPreChargeSeconds, resolutionRatio)
	info.PriceData = priceData
	info.PriceData.AddOtherRatio("seconds", taskgemini.OmniPreChargeSeconds)
	info.PriceData.AddOtherRatio("resolution", resolutionRatio)
	info.PriceData.Quota = preQuota
	if clamp != nil {
		info.QuotaClamp = clamp
	}
	info.Action = common.GetStringIfEmpty(req.Task, "interaction")

	if !info.PriceData.FreeModel {
		info.ForcePreConsume = true
		if apiErr := service.PreConsumeBilling(c, preQuota, info); apiErr != nil {
			omniError(c, apiErr.StatusCode, apiErr.Error())
			return
		}
	}
	settled := false
	defer func() {
		if !settled && info.Billing != nil {
			info.Billing.Refund(c)
		}
	}()

	upstreamBody, err := taskgemini.BuildOmniUpstreamBody(body, info.UpstreamModelName)
	if err != nil {
		omniError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Không huỷ theo client: nếu client ngắt giữa chừng, Google vẫn có thể đã nhận
	// việc, nên cổng vẫn đọc phản hồi để lưu interaction và giữ đúng tiền.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), omniCreateTimeout)
	defer cancel()
	upstreamURL := omniChannelBaseURL(info.ChannelBaseUrl) + "/" + taskgemini.OmniUpstreamAPIVersion + "/interactions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, strings.NewReader(string(upstreamBody)))
	if err != nil {
		omniError(c, http.StatusInternalServerError, "build upstream request failed")
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-goog-api-key", info.ApiKey)
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		omniError(c, http.StatusInternalServerError, "create upstream client failed")
		return
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		logger.LogError(c, "gemini omni upstream request failed: "+err.Error())
		omniError(c, http.StatusBadGateway, "upstream request failed")
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, omniMaxResponseBytes))
	if err != nil {
		omniError(c, http.StatusBadGateway, "read upstream response failed")
		return
	}

	// Google trả lỗi → không tính tiền (defer hoàn trừ trước), chuyển nguyên lỗi.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.Data(resp.StatusCode, "application/json", respBody)
		return
	}
	var interaction taskgemini.OmniInteraction
	if err := common.Unmarshal(respBody, &interaction); err != nil || interaction.ID == "" {
		omniError(c, http.StatusBadGateway, "unexpected upstream response (no interaction id)")
		return
	}

	rewritten, fileIDs := taskgemini.RewriteOmniFileURIs(respBody, omniGatewayBase(c), interaction.ID)
	taskStatus := taskgemini.OmniTaskStatus(interaction.Status)
	if taskStatus == model.TaskStatusFailure {
		// Interaction thất bại → không tính tiền, không cần lưu.
		c.Data(resp.StatusCode, "application/json", rewritten)
		return
	}

	summary := taskgemini.OmniTaskSummary{
		InteractionID: interaction.ID,
		Status:        interaction.Status,
		Resolution:    req.Resolution,
		Seconds:       taskgemini.OmniPreChargeSeconds,
		SecondsSource: "estimate",
	}
	for _, id := range fileIDs {
		summary.Files = append(summary.Files, "files/"+id)
	}
	progress := taskgemini.OmniProgressAwaitingDuration
	finalQuota := preQuota
	switch taskStatus {
	case model.TaskStatusSuccess:
		if seconds, source, ok := taskgemini.OmniBilledSeconds(respBody, &interaction, resolutionRatio); ok {
			quota, quotaClamp := taskgemini.OmniQuota(priceData.ModelPrice, groupRatio, seconds, resolutionRatio)
			if quotaClamp != nil {
				info.QuotaClamp = quotaClamp
			}
			finalQuota = quota
			summary.Seconds = seconds
			summary.SecondsSource = source
			info.PriceData.AddOtherRatio("seconds", seconds)
			progress = taskcommon.ProgressComplete
		}
	default:
		// Đang chạy nền: giữ mức trừ trước; GET interaction hoặc bộ poll quyết toán sau.
		progress = taskcommon.ProgressInProgress
	}
	if info.PriceData.FreeModel {
		finalQuota = 0
	}
	info.PriceData.Quota = finalQuota

	if err := service.SettleBilling(c, info, finalQuota); err != nil {
		common.SysError("settle gemini omni billing error: " + err.Error())
	}
	settled = true
	service.LogTaskConsumption(c, info)

	task := model.InitTask(constant.TaskPlatformGeminiOmni, info)
	task.TaskID = interaction.ID
	task.Status = taskStatus
	task.Progress = progress
	task.Quota = finalQuota
	task.Action = info.Action
	task.StartTime = task.SubmitTime
	if taskStatus == model.TaskStatusSuccess {
		task.FinishTime = time.Now().Unix()
	}
	task.PrivateData.UpstreamTaskID = interaction.ID
	task.PrivateData.BillingSource = info.BillingSource
	task.PrivateData.SubscriptionId = info.SubscriptionId
	task.PrivateData.TokenId = info.TokenId
	task.PrivateData.NodeName = common.NodeName
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		ModelPrice:      priceData.ModelPrice,
		GroupRatio:      groupRatio,
		OtherRatios:     info.PriceData.OtherRatios(),
		OriginModelName: info.OriginModelName,
	}
	task.SetData(summary)
	if err := task.Insert(); err != nil {
		// Mất dòng này thì previous_interaction_id và tải tệp không tìm được kênh.
		common.SysError("insert gemini omni task error: " + err.Error())
	}

	c.Data(resp.StatusCode, "application/json", rewritten)
}

// omniOwnedTask tìm interaction của người dùng hiện tại.
func omniOwnedTask(c *gin.Context, interactionID string) (*model.Task, bool) {
	task, exists, err := model.GetByTaskId(c.GetInt("id"), interactionID)
	if err != nil {
		omniError(c, http.StatusInternalServerError, "query interaction failed")
		return nil, false
	}
	if !exists || task.Platform != constant.TaskPlatformGeminiOmni {
		omniError(c, http.StatusNotFound, "interaction not found for this account")
		return nil, false
	}
	return task, true
}

// omniUpstream trả gốc URL, key và http client của kênh đã tạo interaction.
// Kênh bị tắt vẫn đọc được: tệp/interaction vẫn sống dưới key đó.
func omniUpstream(task *model.Task) (string, string, *http.Client, error) {
	ch, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return "", "", nil, fmt.Errorf("channel %d not found", task.ChannelId)
	}
	key := task.PrivateData.Key
	if key == "" {
		key, _, _ = ch.GetNextEnabledKey()
	}
	if key == "" {
		return "", "", nil, fmt.Errorf("no key stored for interaction")
	}
	client, err := service.GetHttpClientWithProxy(ch.GetSetting().Proxy)
	if err != nil {
		return "", "", nil, err
	}
	return omniChannelBaseURL(ch.GetBaseURL()), key, client, nil
}

// GetGeminiInteraction: GET /v1beta/interactions/{id}.
func GetGeminiInteraction(c *gin.Context) {
	task, ok := omniOwnedTask(c, c.Param("id"))
	if !ok {
		return
	}
	baseURL, key, client, err := omniUpstream(task)
	if err != nil {
		omniError(c, http.StatusBadGateway, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, taskgemini.OmniInteractionURL(baseURL, task.TaskID), nil)
	if err != nil {
		omniError(c, http.StatusInternalServerError, "build upstream request failed")
		return
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-goog-api-key", key)
	resp, err := client.Do(httpReq)
	if err != nil {
		omniError(c, http.StatusBadGateway, "upstream request failed")
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, omniMaxResponseBytes))
	if err != nil {
		omniError(c, http.StatusBadGateway, "read upstream response failed")
		return
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		omniApplyInteractionResult(c.Request.Context(), task, respBody)
	}
	rewritten, _ := taskgemini.RewriteOmniFileURIs(respBody, omniGatewayBase(c), task.TaskID)
	c.Data(resp.StatusCode, "application/json", rewritten)
}

// CancelGeminiInteraction: POST /v1beta/interactions/{id}:cancel.
// Google xác nhận huỷ → hoàn tiền trừ trước (CAS nên chỉ một lần, kể cả khi bộ
// poll hoặc lượt GET khác cũng thấy trạng thái cancelled).
func CancelGeminiInteraction(c *gin.Context) {
	param := c.Param("id")
	if !strings.HasSuffix(param, ":cancel") {
		omniError(c, http.StatusNotFound, "unsupported interaction action")
		return
	}
	task, ok := omniOwnedTask(c, strings.TrimSuffix(param, ":cancel"))
	if !ok {
		return
	}
	baseURL, key, client, err := omniUpstream(task)
	if err != nil {
		omniError(c, http.StatusBadGateway, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 60*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, taskgemini.OmniInteractionURL(baseURL, task.TaskID)+":cancel", strings.NewReader("{}"))
	if err != nil {
		omniError(c, http.StatusInternalServerError, "build upstream request failed")
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", key)
	resp, err := client.Do(httpReq)
	if err != nil {
		omniError(c, http.StatusBadGateway, "upstream request failed")
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, omniMaxResponseBytes))
	if err != nil {
		omniError(c, http.StatusBadGateway, "read upstream response failed")
		return
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Phản hồi huỷ là interaction (status cancelled); nếu Google chưa kịp đổi
		// trạng thái thì lượt GET / bộ poll sau sẽ thấy cancelled và hoàn tiền.
		omniApplyInteractionResult(ctx, task, respBody)
	}
	rewritten, _ := taskgemini.RewriteOmniFileURIs(respBody, omniGatewayBase(c), task.TaskID)
	c.Data(resp.StatusCode, "application/json", rewritten)
}

// omniApplyInteractionResult: khi client tự hỏi interaction nền đã xong, cập nhật
// task và quyết toán ngay (cùng CAS như bộ poll nên không bao giờ tính hai lần).
func omniApplyInteractionResult(ctx context.Context, task *model.Task, respBody []byte) {
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
		return
	}
	adaptor := &taskgemini.OmniTaskAdaptor{}
	result, err := adaptor.ParseTaskResult(respBody)
	if err != nil || (result.Status != model.TaskStatusSuccess && result.Status != model.TaskStatusFailure) {
		return
	}
	previous := task.Status
	task.Status = model.TaskStatus(result.Status)
	task.Progress = common.GetStringIfEmpty(result.Progress, taskcommon.ProgressComplete)
	task.FinishTime = time.Now().Unix()
	task.FailReason = result.Reason
	task.Data = respBody
	won, err := task.UpdateWithStatus(previous)
	if err != nil || !won {
		return
	}
	if task.Status == model.TaskStatusFailure {
		service.RefundTaskQuota(ctx, task, task.FailReason)
		return
	}
	if quota := adaptor.AdjustBillingOnComplete(task, result); quota > 0 {
		service.RecalculateTaskQuota(ctx, task, quota, "gemini omni: usage")
	}
}

// omniFindFileTask tìm interaction (của người dùng hiện tại) đã sinh ra tệp.
func omniFindFileTask(c *gin.Context, fileID string) *model.Task {
	if interactionID := c.Query("interaction_id"); interactionID != "" {
		task, exists, err := model.GetByTaskId(c.GetInt("id"), interactionID)
		if err == nil && exists && taskgemini.OmniTaskOwnsFile(task, fileID) {
			return task
		}
		return nil
	}
	since := time.Now().Add(-omniFileLookupWindow).Unix()
	tasks, err := model.GetRecentUserTasksByPlatform(c.GetInt("id"), constant.TaskPlatformGeminiOmni, since, omniFileLookupLimit)
	if err != nil {
		return nil
	}
	for _, task := range tasks {
		if taskgemini.OmniTaskOwnsFile(task, fileID) {
			return task
		}
	}
	return nil
}

// omniSettleFromFileDuration: tác vụ mới tính tạm 10 s → khi biết thời lượng
// thật của tệp thì trả lại phần thừa (đúng một lần nhờ CAS trên progress).
func omniSettleFromFileDuration(ctx context.Context, task *model.Task, seconds float64, source string) {
	seconds = taskgemini.OmniFileSeconds(seconds)
	bc := task.PrivateData.BillingContext
	if seconds <= 0 || bc == nil || task.Status != model.TaskStatusSuccess || task.Progress != taskgemini.OmniProgressAwaitingDuration {
		return
	}
	won, err := task.UpdateProgressCAS(taskgemini.OmniProgressAwaitingDuration, taskcommon.ProgressComplete)
	if err != nil || !won {
		return
	}
	resolutionRatio := bc.OtherRatios["resolution"]
	quota, clamp := taskgemini.OmniQuota(bc.ModelPrice, bc.GroupRatio, seconds, resolutionRatio)
	if quota <= 0 {
		return
	}
	if bc.OtherRatios != nil {
		bc.OtherRatios["seconds"] = seconds
	}
	service.RecalculateTaskQuota(ctx, task, quota, "gemini omni: thời lượng thật từ "+source, clamp)
}

// RelayGeminiFile: GET /v1beta/files/{id} và GET /v1beta/files/{id}:download.
func RelayGeminiFile(c *gin.Context) {
	name := c.Param("name")
	download := strings.HasSuffix(name, ":download")
	fileID := strings.TrimSuffix(name, ":download")
	if !taskgemini.ValidOmniFileID(fileID) {
		omniError(c, http.StatusBadRequest, "invalid file id")
		return
	}
	task := omniFindFileTask(c, fileID)
	if task == nil {
		omniError(c, http.StatusNotFound, "file not found for this account")
		return
	}
	baseURL, key, client, err := omniUpstream(task)
	if err != nil {
		omniError(c, http.StatusBadGateway, err.Error())
		return
	}

	upstreamURL := baseURL + "/" + taskgemini.OmniUpstreamAPIVersion + "/files/" + fileID
	timeout := 60 * time.Second
	if download {
		upstreamURL += ":download?alt=media"
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
	if err != nil {
		omniError(c, http.StatusInternalServerError, "build upstream request failed")
		return
	}
	httpReq.Header.Set("x-goog-api-key", key)
	if rangeHeader := c.GetHeader("Range"); download && rangeHeader != "" {
		httpReq.Header.Set("Range", rangeHeader)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		omniError(c, http.StatusBadGateway, "upstream request failed")
		return
	}
	defer resp.Body.Close()

	if !download || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			omniError(c, http.StatusBadGateway, "read upstream response failed")
			return
		}
		if resp.StatusCode == http.StatusOK {
			if seconds, ok := omniFileMetadataSeconds(respBody); ok {
				omniSettleFromFileDuration(c.Request.Context(), task, seconds, "file_metadata")
			}
		}
		rewritten, _ := taskgemini.RewriteOmniFileURIs(respBody, omniGatewayBase(c), task.TaskID)
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		c.Data(resp.StatusCode, contentType, rewritten)
		return
	}

	for _, header := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if v := resp.Header.Get(header); v != "" {
			c.Writer.Header().Set(header, v)
		}
	}
	c.Writer.Header().Set("Cache-Control", "private, max-age=86400")
	c.Writer.WriteHeader(resp.StatusCode)

	// Tải trọn tệp lần đầu khi còn tính tạm → đọc lướt mvhd để trả lại phần thừa.
	measure := resp.StatusCode == http.StatusOK && task.Progress == taskgemini.OmniProgressAwaitingDuration
	var probe taskgemini.MP4DurationProbe
	var dst io.Writer = c.Writer
	if measure {
		dst = io.MultiWriter(c.Writer, &probe)
	}
	if _, err := io.Copy(dst, resp.Body); err != nil {
		logger.LogError(c, "stream gemini omni file failed: "+err.Error())
		return
	}
	if seconds, ok := probe.Seconds(); measure && ok {
		omniSettleFromFileDuration(c.Request.Context(), task, seconds, "mp4")
	}
}

// omniFileMetadataSeconds đọc videoMetadata.videoDuration ("8.04s") của Files API nếu có.
func omniFileMetadataSeconds(body []byte) (float64, bool) {
	var meta struct {
		VideoMetadata *struct {
			VideoDuration string `json:"videoDuration"`
		} `json:"videoMetadata"`
	}
	if err := common.Unmarshal(body, &meta); err != nil || meta.VideoMetadata == nil {
		return 0, false
	}
	d, err := time.ParseDuration(strings.TrimSpace(meta.VideoMetadata.VideoDuration))
	if err != nil || d <= 0 {
		return 0, false
	}
	return d.Seconds(), true
}
