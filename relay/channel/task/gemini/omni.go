package gemini

// Gemini Omni Flash qua Interactions API (POST /v1beta/interactions).
//
// Cổng chuyển tiếp nguyên dạng Google (controller/gemini_interactions.go) và
// tính tiền theo GIÂY video đầu ra × hệ số độ phân giải:
//
//	quota = ModelPrice (USD/giây ở 720p) × QuotaPerUnit × tỉ lệ nhóm × giây × hệ số
//
// Hệ số lấy theo bảng giá niêm yết của Google (360p $0.03, 720p $0.10,
// 1080p $0.15, 4k $0.30 mỗi giây) nên chủ cổng chỉ cần đặt một giá
// ModelPrice cho 720p (giá Google + lãi) là mọi độ phân giải theo cùng tỉ lệ.
//
// Số giây lấy theo thứ tự tin cậy:
//  1. usage.output_tokens_by_modality[video] — Google tính tiền theo token
//     (5.792 token cho mỗi giây 720p), nên giây = token / (5792 × hệ số);
//     tiền vì thế đúng bằng tiền Google thu × lãi, ở mọi độ phân giải.
//  2. Thời lượng thật đọc từ tệp MP4 (base64 trả kèm, metadata tệp, hoặc lúc
//     tải về qua cổng) — chỉ dùng để TRẢ LẠI phần trừ trước thừa.
//  3. Không biết: giữ mức trừ trước OmniPreChargeSeconds (10 s, tối đa một lượt).
//
// Cổng KHÔNG dùng interaction nền (background) của Google: GET
// /v1beta/interactions/{id} của interaction nền luôn bị Google từ chối khi xác
// thực bằng API key ("Multiple authentication credentials received"), nên
// không bao giờ đọc lại được kết quả. Thay vào đó:
//
//   - POST của client được trả NGAY (api.kimbox.studio đi qua Cloudflare Tunnel
//     cắt mọi yêu cầu HTTP sau ~100 s) với id do cổng cấp ("gw_…") và
//     status in_progress;
//   - cổng tự gọi Google bằng POST CHẶN (không background, delivery "uri") trong
//     goroutine, lưu phản hồi hoàn tất vào dòng tasks rồi quyết toán;
//   - GET /v1beta/interactions/{id} đọc từ dòng tasks, không gọi Google.
//
// Mỗi interaction là một dòng tasks (platform gemini-omni, task_id = id cổng,
// private_data.upstream_task_id = id interaction Google khi đã biết) để: dính
// kênh/key cho tải tệp và previous_interaction_id; kiểm tra chủ sở hữu; và
// quyết toán ĐÚNG MỘT LẦN (CAS trên trạng thái task) dù có huỷ, bộ poll dọn
// tác vụ treo (cổng khởi động lại giữa chừng) hay kết quả về muộn.

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const (
	// OmniTokensPerSecond720p: Google tính 5.792 token đầu ra cho mỗi giây video 720p.
	OmniTokensPerSecond720p = 5792.0
	// OmniPreChargeSeconds: một lượt Omni ra 3–10 s; trừ trước mức tối đa rồi trả lại phần thừa.
	OmniPreChargeSeconds = 10.0
	// OmniDefaultResolution: độ phân giải mặc định của Google khi không gửi.
	OmniDefaultResolution = "720p"
	// OmniProgressAwaitingDuration: tác vụ đã xong nhưng mới tính tạm 10 s,
	// chờ biết thời lượng thật (metadata tệp / tải về) để trả lại phần thừa.
	OmniProgressAwaitingDuration = "99%"
	// OmniUpstreamAPIVersion: Interactions API chỉ có ở v1beta.
	OmniUpstreamAPIVersion = "v1beta"
)

// omniResolutionRatios: giá mỗi giây so với 720p theo bảng niêm yết của Google.
var omniResolutionRatios = map[string]float64{
	"360p":  0.3,
	"720p":  1,
	"1080p": 1.5,
	"4k":    3,
}

// OmniResolutionRatio trả hệ số giá của độ phân giải (đã chuẩn hoá chữ thường).
func OmniResolutionRatio(resolution string) (float64, bool) {
	ratio, ok := omniResolutionRatios[resolution]
	return ratio, ok
}

// OmniRequest là các trường cổng cần đọc từ thân yêu cầu; phần còn lại chuyển nguyên.
type OmniRequest struct {
	Model                 string
	PreviousInteractionID string
	Resolution            string
	Task                  string // generation_config.video_config.task
}

type omniRequestFields struct {
	Model                 string `json:"model"`
	PreviousInteractionID string `json:"previous_interaction_id"`
	ResponseFormat        *struct {
		Type       string `json:"type"`
		Resolution string `json:"resolution"`
	} `json:"response_format"`
	GenerationConfig *struct {
		VideoConfig *struct {
			Task string `json:"task"`
		} `json:"video_config"`
	} `json:"generation_config"`
}

// ParseOmniRequest đọc và kiểm tra yêu cầu Interactions. Độ phân giải là hệ số
// nhân tiền nên phải thuộc bảng giá; chỉ nhận đầu ra video vì cổng tính tiền theo giây video.
func ParseOmniRequest(body []byte) (*OmniRequest, error) {
	var fields omniRequestFields
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	req := &OmniRequest{
		Model:                 strings.TrimSpace(fields.Model),
		PreviousInteractionID: strings.TrimSpace(fields.PreviousInteractionID),
		Resolution:            OmniDefaultResolution,
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if rf := fields.ResponseFormat; rf != nil {
		if rf.Type != "" && !strings.EqualFold(rf.Type, "video") {
			return nil, fmt.Errorf("response_format.type must be \"video\" on this gateway")
		}
		if rf.Resolution != "" {
			req.Resolution = strings.ToLower(strings.TrimSpace(rf.Resolution))
		}
	}
	if _, ok := OmniResolutionRatio(req.Resolution); !ok {
		return nil, fmt.Errorf("unsupported response_format.resolution %q (360p, 720p, 1080p, 4k)", req.Resolution)
	}
	if gc := fields.GenerationConfig; gc != nil && gc.VideoConfig != nil {
		req.Task = strings.TrimSpace(gc.VideoConfig.Task)
	}
	return req, nil
}

// BuildOmniUpstreamBody giữ nguyên thân yêu cầu, chỉ đổi:
//   - model → tên mô hình phía Google (sau ánh xạ của kênh);
//   - background: luôn bỏ (cổng gọi chặn trong goroutine; xem đầu tệp);
//   - previous_interaction_id → id interaction Google (client gửi id cổng);
//   - store: bỏ false — interaction phải được lưu thì lượt sau mới sửa/nối tiếp được;
//   - response_format.type mặc định "video", delivery luôn "uri" (video tải sau
//     qua cổng; phản hồi hỏi trạng thái được lưu vào CSDL nên không chứa base64).
//   - response_format.duration bị bỏ: Google không nhận ("Invalid input at
//     'response_format'"), độ dài do mô hình tự chọn (3-10 s).
//
// drop: các khoá tuỳ chọn của response_format phải bỏ (gửi lại khi Google từ chối).
func BuildOmniUpstreamBody(body []byte, upstreamModel, previousInteractionID string, drop ...string) ([]byte, error) {
	var root map[string]json.RawMessage
	if err := common.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	modelJSON, err := common.Marshal(upstreamModel)
	if err != nil {
		return nil, err
	}
	root["model"] = modelJSON
	delete(root, "background")
	if previousInteractionID != "" {
		prevJSON, err := common.Marshal(previousInteractionID)
		if err != nil {
			return nil, err
		}
		root["previous_interaction_id"] = prevJSON
	}
	if raw, ok := root["store"]; ok && string(raw) == "false" {
		delete(root, "store")
	}

	responseFormat := map[string]json.RawMessage{}
	if raw, ok := root["response_format"]; ok && string(raw) != "null" {
		if err := common.Unmarshal(raw, &responseFormat); err != nil {
			return nil, fmt.Errorf("response_format must be an object: %w", err)
		}
	}
	if _, ok := responseFormat["type"]; !ok {
		responseFormat["type"] = json.RawMessage(`"video"`)
	}
	responseFormat["delivery"] = json.RawMessage(`"uri"`)
	delete(responseFormat, "duration")
	for _, key := range drop {
		delete(responseFormat, key)
	}
	rfJSON, err := common.Marshal(responseFormat)
	if err != nil {
		return nil, err
	}
	root["response_format"] = rfJSON
	return common.Marshal(root)
}

// OmniInteraction là phần phản hồi Interactions mà cổng cần đọc.
type OmniInteraction struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Usage  *struct {
		OutputTokensByModality []struct {
			Modality string `json:"modality"`
			Tokens   int    `json:"tokens"`
		} `json:"output_tokens_by_modality"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// VideoOutputTokens trả số token video đầu ra Google ghi trong usage.
func (i *OmniInteraction) VideoOutputTokens() int {
	if i == nil || i.Usage == nil {
		return 0
	}
	total := 0
	for _, m := range i.Usage.OutputTokensByModality {
		if strings.EqualFold(m.Modality, "video") && m.Tokens > 0 {
			total += m.Tokens
		}
	}
	return total
}

// OmniTaskStatus đổi trạng thái interaction của Google sang trạng thái task.
// Trạng thái lạ coi như đang chạy để không trả tiền nhầm.
func OmniTaskStatus(status string) model.TaskStatus {
	switch strings.ToLower(status) {
	case "completed":
		return model.TaskStatusSuccess
	case "failed", "cancelled", "incomplete", "budget_exceeded":
		return model.TaskStatusFailure
	case "queued":
		return model.TaskStatusQueued
	default: // in_progress, requires_action, ...
		return model.TaskStatusInProgress
	}
}

// OmniQuota: ModelPrice (USD/giây 720p) × QuotaPerUnit × tỉ lệ nhóm × giây × hệ số độ phân giải.
func OmniQuota(modelPrice, groupRatio, seconds, resolutionRatio float64) (int, *common.QuotaClamp) {
	if modelPrice <= 0 || groupRatio <= 0 || seconds <= 0 || resolutionRatio <= 0 {
		return 0, nil
	}
	return common.QuotaFromFloatChecked(modelPrice * common.QuotaPerUnit * groupRatio * seconds * resolutionRatio)
}

// OmniSecondsFromTokens đổi token video đầu ra sang giây (đã chặn trên).
func OmniSecondsFromTokens(videoTokens int, resolutionRatio float64) float64 {
	if videoTokens <= 0 || resolutionRatio <= 0 {
		return 0
	}
	return math.Min(float64(videoTokens)/(OmniTokensPerSecond720p*resolutionRatio), relaycommon.MaxTaskDurationSeconds)
}

// OmniFileSeconds chặn thời lượng đo từ tệp: chỉ được làm GIẢM so với mức trừ trước,
// vì video ghép (extend) có thể dài hơn phần Google thực tính.
func OmniFileSeconds(seconds float64) float64 {
	if seconds <= 0 || math.IsNaN(seconds) {
		return 0
	}
	return math.Min(seconds, OmniPreChargeSeconds)
}

// OmniBilledSeconds tìm số giây tính tiền trong phản hồi đã hoàn tất.
// source là "usage" hoặc "mp4"; ok=false khi chưa biết (tính tạm 10 s).
func OmniBilledSeconds(respBody []byte, interaction *OmniInteraction, resolutionRatio float64) (seconds float64, source string, ok bool) {
	if s := OmniSecondsFromTokens(interaction.VideoOutputTokens(), resolutionRatio); s > 0 {
		return s, "usage", true
	}
	for _, data := range omniInlineVideos(respBody) {
		if d, found := MP4DurationSeconds(data); found {
			if s := OmniFileSeconds(d); s > 0 {
				return s, "mp4", true
			}
		}
	}
	return 0, "", false
}

// omniInlineVideos lấy các video base64 (delivery "base64") trong phản hồi.
func omniInlineVideos(respBody []byte) [][]byte {
	var root any
	if err := common.Unmarshal(respBody, &root); err != nil {
		return nil
	}
	var out [][]byte
	var walk func(v any)
	walk = func(v any) {
		switch node := v.(type) {
		case map[string]any:
			if t, _ := node["type"].(string); t == "video" {
				if data, _ := node["data"].(string); data != "" {
					if decoded, err := decodeBase64Loose(data); err == nil {
						out = append(out, decoded)
					}
				}
			}
			for _, child := range node {
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(root)
	return out
}

// omniFileURLPattern khớp URI tệp do Google trả, ví dụ
// https://generativelanguage.googleapis.com/v1beta/files/abc123:download?alt=media
var omniFileURLPattern = regexp.MustCompile(`https://generativelanguage\.googleapis\.com/v1(?:beta|alpha)?/files/([A-Za-z0-9_-]+)(:download)?(?:\?[^"\s\\]*)?`)

// omniGoogleFilesBase: gốc URI tệp Google (RewriteOmniFileURIs đổi sang URL cổng).
const omniGoogleFilesBase = "https://generativelanguage.googleapis.com/v1beta/files/"

// omniFileIDPattern khớp tên tệp "files/<id>" trong dữ liệu task đã lưu.
var omniFileIDPattern = regexp.MustCompile(`files/([A-Za-z0-9_-]+)`)

// omniFileIDOnly kiểm tra id tệp người dùng gửi lên.
var omniFileIDOnly = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// ValidOmniFileID cho biết chuỗi có đúng dạng id tệp Google không.
func ValidOmniFileID(id string) bool {
	return omniFileIDOnly.MatchString(id)
}

// RewriteOmniFileURIs thay mọi URI tệp Google bằng URL của cổng để client tải
// bằng token cổng, không cần key Google. Trả kèm danh sách id tệp tìm thấy.
//
//	https://generativelanguage.googleapis.com/v1beta/files/ID:download?alt=media
//	→ {gateway}/v1beta/files/ID:download?alt=media&interaction_id=IID
func RewriteOmniFileURIs(body []byte, gatewayBase, interactionID string) ([]byte, []string) {
	gatewayBase = strings.TrimRight(gatewayBase, "/")
	var fileIDs []string
	seen := map[string]bool{}
	rewritten := omniFileURLPattern.ReplaceAllFunc(body, func(match []byte) []byte {
		sub := omniFileURLPattern.FindSubmatch(match)
		id := string(sub[1])
		if !seen[id] {
			seen[id] = true
			fileIDs = append(fileIDs, id)
		}
		return []byte(OmniGatewayFileURL(gatewayBase, id, len(sub[2]) > 0, interactionID))
	})
	return rewritten, fileIDs
}

// OmniGatewayFileURL dựng URL tệp phía cổng.
func OmniGatewayFileURL(gatewayBase, fileID string, download bool, interactionID string) string {
	u := strings.TrimRight(gatewayBase, "/") + "/v1beta/files/" + fileID
	if download {
		u += ":download?alt=media"
		if interactionID != "" {
			u += "&interaction_id=" + url.QueryEscape(interactionID)
		}
		return u
	}
	if interactionID != "" {
		u += "?interaction_id=" + url.QueryEscape(interactionID)
	}
	return u
}

// OmniFileIDs liệt kê id tệp xuất hiện trong dữ liệu task đã lưu (phản hồi
// Google gốc hoặc bản tóm tắt của cổng).
func OmniFileIDs(data []byte) []string {
	var ids []string
	for _, m := range omniFileIDPattern.FindAllSubmatch(data, -1) {
		ids = append(ids, string(m[1]))
	}
	return ids
}

// OmniTaskOwnsFile: tệp có thuộc interaction đã lưu không (khớp đúng cả id).
func OmniTaskOwnsFile(task *model.Task, fileID string) bool {
	if task == nil || task.Platform != constant.TaskPlatformGeminiOmni {
		return false
	}
	for _, id := range OmniFileIDs(task.Data) {
		if id == fileID {
			return true
		}
	}
	return false
}

// OmniTaskSummary là phần dữ liệu cổng lưu vào tasks.data cho mỗi interaction.
type OmniTaskSummary struct {
	InteractionID string   `json:"interaction_id"`
	Status        string   `json:"status"`
	Resolution    string   `json:"resolution"`
	Seconds       float64  `json:"seconds,omitempty"`
	SecondsSource string   `json:"seconds_source,omitempty"` // usage | mp4 | file_metadata | estimate
	Files         []string `json:"files,omitempty"`          // dạng "files/<id>"
	Model         string   `json:"model,omitempty"`
	// Error: lý do thất bại/huỷ (status failed | cancelled).
	Error *OmniTaskError `json:"error,omitempty"`
}

// OmniTaskError là lỗi lưu kèm interaction thất bại (dạng error của Google).
type OmniTaskError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message"`
}

// IsOmniGatewayID: id interaction do cổng cấp (không phải id Google của dòng cũ).
func IsOmniGatewayID(id string) bool {
	return strings.HasPrefix(id, constant.GeminiOmniTaskIDPrefix)
}

// OmniGoogleInteractionID trả id interaction Google của dòng task ("" khi
// chưa biết: đang chạy, thất bại, huỷ). Dòng cũ dùng chính id Google làm task_id.
func OmniGoogleInteractionID(task *model.Task) string {
	if task.PrivateData.UpstreamTaskID != "" {
		return task.PrivateData.UpstreamTaskID
	}
	if IsOmniGatewayID(task.TaskID) {
		return ""
	}
	return task.TaskID
}

// omniFailedStatuses: trạng thái kết thúc không thành công của Google.
var omniFailedStatuses = map[string]bool{"failed": true, "cancelled": true, "incomplete": true, "budget_exceeded": true}

// OmniInteractionView dựng phản hồi GET /v1beta/interactions/{id} chỉ từ dòng
// task (không gọi Google):
//   - đang chạy → {"id","object","model","status":"in_progress"};
//   - hoàn tất → phản hồi Google đã lưu (nguyên dạng), id thay bằng id cổng,
//     URI tệp viết lại trỏ về cổng kèm interaction_id = id cổng;
//   - thất bại/huỷ → {"id","object","model","status":"failed|cancelled","error":{…}}.
func OmniInteractionView(task *model.Task, gatewayBase string) []byte {
	head := map[string]any{"id": task.TaskID, "object": "interaction"}
	var summary OmniTaskSummary
	_ = common.Unmarshal(task.Data, &summary)
	if m := common.GetStringIfEmpty(summary.Model, task.Properties.OriginModelName); m != "" {
		head["model"] = m
	}
	switch task.Status {
	case model.TaskStatusSuccess:
		var stored map[string]json.RawMessage
		if err := common.Unmarshal(task.Data, &stored); err == nil {
			if _, isSummary := stored["interaction_id"]; !isSummary {
				idJSON, _ := common.Marshal(task.TaskID)
				stored["id"] = idJSON
				if out, err := common.Marshal(stored); err == nil {
					rewritten, _ := RewriteOmniFileURIs(out, gatewayBase, task.TaskID)
					return rewritten
				}
			}
		}
		// Dòng cũ hoàn tất ngay lúc tạo chỉ lưu bản tóm tắt: dựng lại nội dung video từ danh sách tệp.
		head["status"] = "completed"
		var content []map[string]any
		for _, name := range summary.Files {
			id := strings.TrimPrefix(name, "files/")
			content = append(content, map[string]any{"type": "video", "mime_type": "video/mp4", "uri": omniGoogleFilesBase + id + ":download?alt=media"})
		}
		if len(content) > 0 {
			head["steps"] = []any{map[string]any{"type": "model_output", "content": content}}
		}
	case model.TaskStatusFailure, model.TaskStatusCancelled:
		status := strings.ToLower(summary.Status)
		if !omniFailedStatuses[status] {
			status = "failed"
		}
		head["status"] = status
		taskErr := OmniTaskError{Message: task.FailReason}
		if summary.Error != nil {
			taskErr = *summary.Error
		}
		if taskErr.Message == "" {
			taskErr.Message = "interaction " + status
		}
		head["error"] = taskErr
	default:
		head["status"] = "in_progress"
	}
	out, _ := common.Marshal(head)
	// Viết lại URI sau khi mã hoá: Marshal thoát "&" trong URL của cổng thành \u0026.
	rewritten, _ := RewriteOmniFileURIs(out, gatewayBase, task.TaskID)
	return rewritten
}

// ============================
// Bộ poll tác vụ nền
// ============================

// OmniTaskAdaptor chỉ để thoả giao diện channel.TaskAdaptor (nhúng TaskAdaptor
// của Veo) và đọc phản hồi interaction (ParseTaskResult / AdjustBillingOnComplete).
// Bộ poll KHÔNG hỏi Google cho tác vụ gemini-omni: kết quả do goroutine của
// controller.RelayGeminiInteraction ghi; bộ poll chỉ dọn tác vụ treo
// (service.finalizeStaleGeminiOmniTasks).
type OmniTaskAdaptor struct {
	TaskAdaptor
}

func (a *OmniTaskAdaptor) GetModelList() []string {
	return []string{"gemini-omni-1.1-flash", "gemini-omni-flash-preview"}
}

func (a *OmniTaskAdaptor) GetChannelName() string {
	return string(constant.TaskPlatformGeminiOmni)
}

// ErrOmniNotPollable: GET interaction của Google từ chối API key với interaction nền.
var ErrOmniNotPollable = fmt.Errorf("gemini-omni interactions are finalized by the gateway, not polled upstream")

// FetchTask không bao giờ gọi Google (xem OmniTaskAdaptor).
func (a *OmniTaskAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, ErrOmniNotPollable
}

// ParseTaskResult đổi phản hồi interaction sang TaskInfo. Lỗi tạm thời của
// Google (429/5xx) giữ trạng thái đang chạy để vòng sau hỏi lại thay vì hoàn tiền nhầm.
func (a *OmniTaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var envelope struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := common.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal interaction failed: %w", err)
	}
	if e := envelope.Error; e != nil {
		if e.Code == http.StatusTooManyRequests || e.Code >= 500 {
			return &relaycommon.TaskInfo{Status: model.TaskStatusInProgress}, nil
		}
		return relaycommon.FailTaskInfo(e.Message), nil
	}

	var interaction OmniInteraction
	if err := common.Unmarshal(respBody, &interaction); err != nil {
		return nil, fmt.Errorf("unmarshal interaction failed: %w", err)
	}
	info := &relaycommon.TaskInfo{TaskID: interaction.ID, Status: string(OmniTaskStatus(interaction.Status))}
	switch info.Status {
	case model.TaskStatusFailure:
		info.Reason = "interaction " + interaction.Status
		if interaction.Error != nil && interaction.Error.Message != "" {
			info.Reason = interaction.Error.Message
		}
	case model.TaskStatusSuccess:
		// Không có usage video → giữ 10 s tạm, chờ thời lượng thật từ tệp.
		if interaction.VideoOutputTokens() <= 0 {
			info.Progress = OmniProgressAwaitingDuration
		}
	}
	return info, nil
}

// AdjustBillingOnComplete tính lại tiền từ usage trong phản hồi cuối (task.Data).
// Trả 0 (giữ mức trừ trước) khi không có usage video.
func (a *OmniTaskAdaptor) AdjustBillingOnComplete(task *model.Task, _ *relaycommon.TaskInfo) int {
	bc := task.PrivateData.BillingContext
	if bc == nil {
		return 0
	}
	var interaction OmniInteraction
	if err := common.Unmarshal(task.Data, &interaction); err != nil {
		return 0
	}
	resolutionRatio := bc.OtherRatios["resolution"]
	seconds := OmniSecondsFromTokens(interaction.VideoOutputTokens(), resolutionRatio)
	if seconds <= 0 {
		return 0
	}
	quota, _ := OmniQuota(bc.ModelPrice, bc.GroupRatio, seconds, resolutionRatio)
	if quota > 0 && bc.OtherRatios != nil {
		// Ghi số giây thật vào ngữ cảnh để nhật ký quyết toán hiển thị đúng.
		bc.OtherRatios["seconds"] = seconds
	}
	return quota
}
