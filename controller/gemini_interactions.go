package controller

// Chuyển tiếp Gemini Omni Flash (Interactions API) nguyên dạng Google cho client
// chỉ có token cổng (KSB):
//
//	POST /v1beta/interactions            trả NGAY {"id":"gw_…","status":"in_progress"};
//	                                     cổng tự gọi Google (POST chặn) trong goroutine
//	GET  /v1beta/interactions/{id}       đọc trạng thái/kết quả từ dòng tasks (không gọi Google)
//	POST /v1beta/interactions/{id}:cancel huỷ phía cổng; hoàn tiền trừ trước (1 lần)
//	GET  /v1beta/files/{id}              metadata tệp (state PROCESSING/ACTIVE/FAILED)
//	GET  /v1beta/files/{id}:download     tải video (truyền luồng)
//
// Vì sao không dùng interaction nền (background) của Google: GET
// /v1beta/interactions/{id} của interaction nền luôn trả 400 "Multiple
// authentication credentials received" với API key, nên không đọc lại được
// video. POST chặn (không background, delivery "uri") thì chạy được nhưng mất
// 20–60 s, quá lâu để giữ yêu cầu của client qua Cloudflare Tunnel (~100 s).
//
// Mọi lời gọi Google dùng ĐÚNG kênh + key đã tạo interaction (lưu ở dòng
// tasks platform gemini-omni) và chỉ chủ sở hữu (cùng user) mới đọc được.
// Cách tính tiền: xem relay/channel/task/gemini/omni.go.

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
	// omniMaxResponseBytes: chặn phản hồi quá lớn.
	omniMaxResponseBytes = 512 << 20
	// omniFileLookupWindow: tệp Google tồn tại 48 giờ.
	omniFileLookupWindow = 48 * time.Hour
	// omniFileLookupLimit: số interaction gần nhất dò khi client không gửi interaction_id.
	omniFileLookupLimit = 500
)

// omniJobs đếm các lời gọi Google đang chạy nền (để test chờ xong).
var omniJobs sync.WaitGroup

// WaitGeminiInteractionJobs chờ mọi lời gọi Google nền của Interactions kết thúc.
func WaitGeminiInteractionJobs() {
	omniJobs.Wait()
}

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

func newOmniGatewayID() (string, error) {
	key, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", err
	}
	return constant.GeminiOmniTaskIDPrefix + key, nil
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

	// Lượt tiếp theo (edit/extend): client gửi id cổng → đổi sang id interaction
	// Google đã lưu, và phải về đúng kênh + key đã tạo nó (interaction gắn với key/project).
	previousGoogleID := ""
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
		previousGoogleID = taskgemini.OmniGoogleInteractionID(prev)
		if previousGoogleID == "" || prev.Status == model.TaskStatusFailure || prev.Status == model.TaskStatusCancelled {
			omniError(c, http.StatusBadRequest, "previous interaction is not completed (still in progress, failed or cancelled)")
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

	// Dựng sẵn thân gửi Google (lỗi thì trả 400 trước khi trừ tiền). Google từ
	// chối resolution (400 nhắc response_format, không tính tiền) thì gửi lại
	// không có resolution. Không bao giờ bỏ delivery "uri": phản hồi được lưu
	// vào CSDL nên không được chứa video base64.
	var upstreamBodies [][]byte
	for _, drop := range [][]string{nil, {"resolution"}} {
		upstreamBody, err := taskgemini.BuildOmniUpstreamBody(body, info.UpstreamModelName, previousGoogleID, drop...)
		if err != nil {
			omniError(c, http.StatusBadRequest, err.Error())
			return
		}
		upstreamBodies = append(upstreamBodies, upstreamBody)
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
	if clamp != nil {
		info.QuotaClamp = clamp
	}
	if info.PriceData.FreeModel {
		preQuota = 0
	}
	info.PriceData.Quota = preQuota
	info.Action = common.GetStringIfEmpty(req.Task, "interaction")

	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		omniError(c, http.StatusInternalServerError, "create upstream client failed")
		return
	}
	// Thời hạn do context của lời gọi quyết định (GeminiOmniUpstreamTimeout),
	// không theo RELAY_TIMEOUT chung.
	jobClient := *client
	jobClient.Timeout = 0

	gatewayID, err := newOmniGatewayID()
	if err != nil {
		omniError(c, http.StatusInternalServerError, "generate interaction id failed")
		return
	}

	if !info.PriceData.FreeModel {
		info.ForcePreConsume = true
		if apiErr := service.PreConsumeBilling(c, preQuota, info); apiErr != nil {
			omniError(c, apiErr.StatusCode, apiErr.Error())
			return
		}
	}

	task := model.InitTask(constant.TaskPlatformGeminiOmni, info)
	task.TaskID = gatewayID
	task.Status = model.TaskStatusInProgress
	task.Progress = taskcommon.ProgressInProgress
	task.Quota = preQuota
	task.Action = info.Action
	task.StartTime = task.SubmitTime
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
	task.SetData(taskgemini.OmniTaskSummary{
		Status:        "in_progress",
		Resolution:    req.Resolution,
		Seconds:       taskgemini.OmniPreChargeSeconds,
		SecondsSource: "estimate",
		Model:         info.OriginModelName,
	})
	if err := task.Insert(); err != nil {
		// Không có dòng task thì không theo dõi/hoàn tiền được → không gửi Google.
		common.SysError("insert gemini omni task error: " + err.Error())
		if info.Billing != nil {
			info.Billing.Refund(c)
		}
		omniError(c, http.StatusInternalServerError, "create interaction failed")
		return
	}

	// Chốt mức trừ trước thành tiền của task; goroutine quyết toán lại theo
	// usage (RecalculateTaskQuota) hoặc hoàn trọn (RefundTaskQuota) — CAS nên 1 lần.
	if err := service.SettleBilling(c, info, preQuota); err != nil {
		common.SysError("settle gemini omni billing error: " + err.Error())
	}
	service.LogTaskConsumption(c, info)

	job := &omniJob{
		ctx:         context.WithoutCancel(c.Request.Context()),
		userID:      task.UserId,
		gatewayID:   gatewayID,
		upstreamURL: omniChannelBaseURL(info.ChannelBaseUrl) + "/" + taskgemini.OmniUpstreamAPIVersion + "/interactions",
		apiKey:      info.ApiKey,
		client:      &jobClient,
		bodies:      upstreamBodies,
	}
	omniJobs.Add(1)
	go job.run()

	c.JSON(http.StatusOK, gin.H{
		"id":     gatewayID,
		"object": "interaction",
		"model":  info.OriginModelName,
		"status": "in_progress",
	})
}

// omniJob là một lời gọi chặn POST /v1beta/interactions tới Google, chạy sau khi
// client đã nhận id. Không dùng *gin.Context (đã trả về pool).
type omniJob struct {
	ctx         context.Context
	userID      int
	gatewayID   string
	upstreamURL string
	apiKey      string
	client      *http.Client
	bodies      [][]byte
}

func (j *omniJob) run() {
	defer omniJobs.Done()
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("gemini omni job %s panic: %v", j.gatewayID, r))
			j.fail(0, "internal error while waiting for the upstream result")
		}
	}()

	ctx, cancel := context.WithTimeout(j.ctx, constant.GeminiOmniUpstreamTimeout)
	defer cancel()
	statusCode, respBody, err := j.call(ctx)
	if err != nil {
		logger.LogError(j.ctx, fmt.Sprintf("gemini omni %s upstream request failed: %s", j.gatewayID, err.Error()))
		j.fail(http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		j.fail(statusCode, omniUpstreamErrorMessage(statusCode, respBody))
		return
	}
	var interaction taskgemini.OmniInteraction
	if err := common.Unmarshal(respBody, &interaction); err != nil || interaction.ID == "" {
		j.fail(http.StatusBadGateway, "unexpected upstream response (no interaction id)")
		return
	}
	switch taskgemini.OmniTaskStatus(interaction.Status) {
	case model.TaskStatusSuccess:
		j.complete(&interaction, respBody)
	case model.TaskStatusFailure:
		reason := "interaction " + interaction.Status
		if interaction.Error != nil && interaction.Error.Message != "" {
			reason = interaction.Error.Message
		}
		j.fail(0, reason)
	default:
		// Lời gọi chặn phải trả trạng thái cuối; trạng thái khác không đọc lại được.
		j.fail(http.StatusBadGateway, "upstream returned non-final status "+interaction.Status)
	}
}

// call gửi POST chặn; Google từ chối response_format (400) thì thử thân kế tiếp.
func (j *omniJob) call(ctx context.Context) (int, []byte, error) {
	var statusCode int
	var respBody []byte
	for i, upstreamBody := range j.bodies {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, j.upstreamURL, strings.NewReader(string(upstreamBody)))
		if err != nil {
			return 0, nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("x-goog-api-key", j.apiKey)
		resp, err := j.client.Do(httpReq)
		if err != nil {
			return 0, nil, err
		}
		respBody, err = io.ReadAll(io.LimitReader(resp.Body, omniMaxResponseBytes))
		resp.Body.Close()
		if err != nil {
			return 0, nil, err
		}
		statusCode = resp.StatusCode
		if statusCode != http.StatusBadRequest || !strings.Contains(string(respBody), "response_format") || i == len(j.bodies)-1 {
			break
		}
		logger.LogWarn(j.ctx, fmt.Sprintf("gemini omni %s: response_format rejected, retrying with fewer options: %s", j.gatewayID, string(respBody)))
	}
	return statusCode, respBody, nil
}

// loadInProgress đọc lại dòng task; nil khi task đã kết thúc (huỷ / bộ poll dọn).
func (j *omniJob) loadInProgress(googleID string) *model.Task {
	task, exists, err := model.GetByTaskId(j.userID, j.gatewayID)
	if err != nil || !exists {
		common.SysError(fmt.Sprintf("gemini omni %s: task row not found (err=%v)", j.gatewayID, err))
		return nil
	}
	if task.Status != model.TaskStatusInProgress {
		// Kết quả về muộn (đã huỷ / đã dọn): giữ nguyên trạng thái và hoàn tiền.
		// Google vẫn có thể đã tính tiền lượt này.
		logger.LogWarn(j.ctx, fmt.Sprintf("gemini omni %s: upstream finished (google id %q) after the task became %s; result ignored, user stays refunded — Google may still bill this generation", j.gatewayID, googleID, task.Status))
		return nil
	}
	return task
}

func (j *omniJob) complete(interaction *taskgemini.OmniInteraction, respBody []byte) {
	task := j.loadInProgress(interaction.ID)
	if task == nil {
		return
	}
	adaptor := &taskgemini.OmniTaskAdaptor{}
	result, err := adaptor.ParseTaskResult(respBody)
	if err != nil {
		j.fail(http.StatusBadGateway, "unexpected upstream response")
		return
	}
	task.Status = model.TaskStatusSuccess
	task.Progress = common.GetStringIfEmpty(result.Progress, taskcommon.ProgressComplete)
	task.FinishTime = time.Now().Unix()
	task.FailReason = ""
	// Lưu nguyên phản hồi Google (chỉ URI, không base64); GET viết lại URI tệp về cổng.
	task.Data = respBody
	task.PrivateData.UpstreamTaskID = interaction.ID
	won, err := task.UpdateWithStatus(model.TaskStatusInProgress)
	if err != nil || !won {
		logger.LogWarn(j.ctx, fmt.Sprintf("gemini omni %s: completion lost the status CAS (err=%v); google id %s", j.gatewayID, err, interaction.ID))
		return
	}
	if task.Quota <= 0 {
		return // mô hình miễn phí
	}
	if quota := adaptor.AdjustBillingOnComplete(task, result); quota > 0 {
		service.RecalculateTaskQuota(j.ctx, task, quota, "gemini omni: usage")
	}
}

// fail đánh thất bại (CAS) và hoàn trọn tiền trừ trước.
func (j *omniJob) fail(code int, reason string) {
	task := j.loadInProgress("")
	if task == nil {
		return
	}
	omniFailTask(j.ctx, task, "failed", code, reason)
}

// omniFailTask chuyển task đang chạy sang thất bại/huỷ và hoàn tiền (đúng 1 lần nhờ CAS).
func omniFailTask(ctx context.Context, task *model.Task, status string, code int, reason string) bool {
	var summary taskgemini.OmniTaskSummary
	_ = common.Unmarshal(task.Data, &summary)
	summary.Status = status
	summary.Error = &taskgemini.OmniTaskError{Code: code, Message: reason}
	previous := task.Status
	task.Status = model.TaskStatusFailure
	task.Progress = taskcommon.ProgressComplete
	task.FinishTime = time.Now().Unix()
	task.FailReason = reason
	task.SetData(summary)
	won, err := task.UpdateWithStatus(previous)
	if err != nil || !won {
		return false
	}
	if task.Quota != 0 {
		service.RefundTaskQuota(ctx, task, reason)
	}
	return true
}

// omniUpstreamErrorMessage lấy error.message của Google, nếu không có thì thân phản hồi.
func omniUpstreamErrorMessage(statusCode int, body []byte) string {
	var envelope struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := common.Unmarshal(body, &envelope); err == nil && envelope.Error != nil && envelope.Error.Message != "" {
		return envelope.Error.Message
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 500 {
		msg = msg[:500]
	}
	if msg == "" {
		msg = http.StatusText(statusCode)
	}
	return fmt.Sprintf("upstream status %d: %s", statusCode, msg)
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
// Kênh bị tắt vẫn đọc được: tệp vẫn sống dưới key đó.
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

// GetGeminiInteraction: GET /v1beta/interactions/{id} — chỉ đọc dòng tasks.
// Nhận cả id cổng ("gw_…") lẫn id Google của các dòng cũ.
func GetGeminiInteraction(c *gin.Context) {
	task, ok := omniOwnedTask(c, c.Param("id"))
	if !ok {
		return
	}
	c.Data(http.StatusOK, "application/json", taskgemini.OmniInteractionView(task, omniGatewayBase(c)))
}

// CancelGeminiInteraction: POST /v1beta/interactions/{id}:cancel.
// Lời gọi chặn tới Google không huỷ được (chưa có id Google), nên cổng chỉ đánh
// dấu huỷ + hoàn tiền (CAS: một lần); kết quả về muộn bị bỏ qua.
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
	if task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure && task.Status != model.TaskStatusCancelled {
		ctx := context.WithoutCancel(c.Request.Context())
		if omniFailTask(ctx, task, "cancelled", 0, "cancelled by client") {
			logger.LogWarn(ctx, fmt.Sprintf("gemini omni %s cancelled while the upstream call may still run; user refunded, Google may still bill it", task.TaskID))
		}
		if fresh, exists, err := model.GetByTaskId(task.UserId, task.TaskID); err == nil && exists {
			task = fresh
		}
	}
	c.Data(http.StatusOK, "application/json", taskgemini.OmniInteractionView(task, omniGatewayBase(c)))
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
