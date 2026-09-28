package controller

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// payOS (payos.vn) - nạp tiền bằng chuyển khoản ngân hàng qua mã VietQR.
//
// Luồng: người dùng chọn số tiền -> gateway tạo đơn chờ + link thanh toán payOS
// -> người dùng quét QR bằng app ngân hàng -> payOS gọi webhook khi tiền vào
// -> gateway kiểm chữ ký, số tiền rồi cộng hạn mức (model.RechargePayOS).
//
// Khi người dùng bấm quay về từ trang payOS, gateway còn tự hỏi payOS trạng thái
// đơn (/api/payos/return). Nhờ vậy nạp vẫn tự động kể cả khi webhook chưa cài
// hoặc tới trễ; hai đường cùng gọi RechargePayOS nên không cộng trùng.

var payOSAPIBase = "https://api-merchant.payos.vn"

var payOSHTTPClient = &http.Client{Timeout: 15 * time.Second}

const (
	payOSTradePrefix = "PAYOS-"
	// Ngân hàng không liên kết qua payOS chỉ nhận 9 ký tự nội dung chuyển khoản.
	payOSDescription = "NAPTIEN"
	payOSLinkTTL     = 15 * time.Minute
	// Giới hạn orderCode của payOS (số nguyên an toàn của JavaScript).
	payOSMaxOrderCode = 9007199254740991
)

func payOSTradeNo(orderCode int64) string {
	return payOSTradePrefix + strconv.FormatInt(orderCode, 10)
}

// newPayOSOrderCode: mili-giây x 1000 + 3 chữ số ngẫu nhiên, khoảng 1,8e15 - dưới
// giới hạn 9e15 của payOS và không trùng khi nhiều người bấm cùng lúc.
func newPayOSOrderCode() int64 {
	return time.Now().UnixMilli()*1000 + int64(rand.Intn(1000))
}

// getPayOSPayMoney đổi số lượng người dùng nhập ra số VND phải trả (làm tròn
// lên đồng). Cách tính giống các cổng khác: tỉ lệ nhóm nạp + giảm giá theo mức.
func getPayOSPayMoney(amount float64, group string) int64 {
	originalAmount := amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = amount / common.QuotaPerUnit
	}
	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(originalAmount)]; ok && ds > 0 {
		discount = ds
	}
	return int64(math.Ceil(amount * setting.PayOSUnitPrice * topupGroupRatio * discount))
}

// payOSSign ký chuỗi theo HMAC-SHA256 bằng checksum key, trả về hex.
func payOSSign(data string) string {
	mac := hmac.New(sha256.New, []byte(setting.PayOSChecksumKey))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// payOSCreateSignature: chữ ký khi tạo link, đúng thứ tự payOS quy định.
func payOSCreateSignature(amount int64, cancelURL, description string, orderCode int64, returnURL string) string {
	return payOSSign(fmt.Sprintf("amount=%d&cancelUrl=%s&description=%s&orderCode=%d&returnUrl=%s",
		amount, cancelURL, description, orderCode, returnURL))
}

// payOSDataString dựng chuỗi ký của object "data" theo tài liệu payOS: khoá sắp
// theo bảng chữ cái, "k=v" nối bằng "&", null thành rỗng, mảng thành JSON với
// từng phần tử đã sắp khoá. Số giữ nguyên dạng gốc (json.Number) để khớp từng ký tự.
func payOSDataString(data map[string]any) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+payOSValueString(data[k]))
	}
	return strings.Join(parts, "&")
}

func payOSValueString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		if value == "null" || value == "undefined" {
			return ""
		}
		return value
	case json.Number:
		return value.String()
	case bool:
		return strconv.FormatBool(value)
	case []any:
		// encoding/json sắp khoá map khi mã hoá, đúng như sortObjDataByKey của payOS.
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(value); err != nil {
			return ""
		}
		return strings.TrimRight(buf.String(), "\n")
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func payOSVerifyData(data map[string]any, signature string) bool {
	if signature == "" || setting.PayOSChecksumKey == "" {
		return false
	}
	expected := payOSSign(payOSDataString(data))
	return hmac.Equal([]byte(expected), []byte(strings.ToLower(signature)))
}

func decodePayOSJSON(body []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	return dec.Decode(out)
}

func payOSNumber(v any) (int64, bool) {
	switch value := v.(type) {
	case json.Number:
		n, err := value.Int64()
		return n, err == nil
	case string:
		n, err := strconv.ParseInt(value, 10, 64)
		return n, err == nil
	}
	return 0, false
}

type payOSEnvelope struct {
	Code      string         `json:"code"`
	Desc      string         `json:"desc"`
	Success   bool           `json:"success"`
	Data      map[string]any `json:"data"`
	Signature string         `json:"signature"`
}

func payOSRequest(ctx context.Context, method, path string, payload any) (*payOSEnvelope, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, payOSAPIBase+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-client-id", setting.PayOSClientId)
	req.Header.Set("x-api-key", setting.PayOSApiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := payOSHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var envelope payOSEnvelope
	if err := decodePayOSJSON(raw, &envelope); err != nil {
		return nil, fmt.Errorf("payOS http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if envelope.Code != "00" {
		return &envelope, fmt.Errorf("payOS code=%s desc=%s", envelope.Code, envelope.Desc)
	}
	return &envelope, nil
}

type PayOSPayRequest struct {
	Amount int64 `json:"amount"`
}

func RequestPayOSAmount(c *gin.Context) {
	var req PayOSPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.Amount < int64(setting.PayOSMinTopUp) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", setting.PayOSMinTopUp)})
		return
	}
	group, err := model.GetUserGroup(c.GetInt("id"), true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayOSPayMoney(float64(req.Amount), group)
	if payMoney < 1 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatInt(payMoney, 10)})
}

// RequestPayOSPay tạo đơn chờ và link thanh toán payOS; trả về payment_url để
// trình duyệt mở trang QR của payOS.
func RequestPayOSPay(c *gin.Context) {
	if !isPayOSTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "payOS chưa được bật"})
		return
	}
	var req PayOSPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.Amount < int64(setting.PayOSMinTopUp) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", setting.PayOSMinTopUp)})
		return
	}

	id := c.GetInt("id")
	group, _ := model.GetUserGroup(id, true)
	payMoney := getPayOSPayMoney(float64(req.Amount), group)
	if payMoney < 1 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	// Token mode: lưu số đơn vị tương đương (giống Waffo) để lúc cộng không nhân hai lần.
	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = int64(float64(req.Amount) / common.QuotaPerUnit)
		if amount < 1 {
			amount = 1
		}
	}

	orderCode := newPayOSOrderCode()
	tradeNo := payOSTradeNo(orderCode)
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           float64(payMoney),
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodPayOS,
		PaymentProvider: model.PaymentProviderPayOS,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("payOS tạo đơn nạp thất bại user_id=%d trade_no=%s error=%q", id, tradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	// Quay về (thanh toán xong hay huỷ) đều qua /api/payos/return để gateway
	// hỏi payOS trạng thái đơn rồi mới chuyển người dùng về trang ví.
	returnURL := service.GetCallbackAddress() + "/api/payos/return"
	payload := map[string]any{
		"orderCode":   orderCode,
		"amount":      payMoney,
		"description": payOSDescription,
		"returnUrl":   returnURL,
		"cancelUrl":   returnURL,
		"expiredAt":   time.Now().Add(payOSLinkTTL).Unix(),
		"signature":   payOSCreateSignature(payMoney, returnURL, payOSDescription, orderCode, returnURL),
	}
	envelope, err := payOSRequest(c.Request.Context(), http.MethodPost, "/v2/payment-requests", payload)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("payOS tạo link thanh toán thất bại user_id=%d trade_no=%s error=%q", id, tradeNo, err.Error()))
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	checkoutURL, _ := envelope.Data["checkoutUrl"].(string)
	if checkoutURL == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("payOS không trả checkoutUrl user_id=%d trade_no=%s", id, tradeNo))
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("payOS tạo đơn nạp thành công user_id=%d trade_no=%s amount=%d money_vnd=%d", id, tradeNo, req.Amount, payMoney))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"payment_url": checkoutURL,
			"order_id":    tradeNo,
		},
	})
}

// completePayOSOrder cộng hạn mức cho một đơn payOS đã trả; mọi lỗi "không làm
// gì được nữa" (không có đơn, thiếu tiền, sai cổng) chỉ ghi log.
func completePayOSOrder(ctx context.Context, orderCode int64, paidVND int64, callerIP string, source string) error {
	tradeNo := payOSTradeNo(orderCode)
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	alreadyDone, err := model.RechargePayOS(tradeNo, paidVND, callerIP)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrTopUpNotFound):
			logger.LogWarn(ctx, fmt.Sprintf("payOS %s: không có đơn trade_no=%s (webhook thử của payOS cũng rơi vào đây)", source, tradeNo))
		case errors.Is(err, model.ErrPaymentAmountMismatch):
			logger.LogWarn(ctx, fmt.Sprintf("payOS %s: chuyển thiếu tiền trade_no=%s paid_vnd=%d", source, tradeNo, paidVND))
		default:
			logger.LogError(ctx, fmt.Sprintf("payOS %s: nạp tiền thất bại trade_no=%s error=%q", source, tradeNo, err.Error()))
		}
		return err
	}
	if alreadyDone {
		logger.LogInfo(ctx, fmt.Sprintf("payOS %s: đơn đã cộng trước đó trade_no=%s", source, tradeNo))
	} else {
		logger.LogInfo(ctx, fmt.Sprintf("payOS %s: nạp tiền thành công trade_no=%s paid_vnd=%d", source, tradeNo, paidVND))
	}
	return nil
}

// PayOSWebhook nhận thông báo "đã nhận tiền" của payOS.
//
// Luôn trả 200 khi chữ ký đúng (kể cả đơn không có hay đã cộng) để payOS không
// gửi lại mãi; chữ ký sai trả 400.
func PayOSWebhook(c *gin.Context) {
	if !isPayOSWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("payOS webhook bị từ chối reason=disabled client_ip=%s", c.ClientIP()))
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	var envelope payOSEnvelope
	if err := decodePayOSJSON(body, &envelope); err != nil || envelope.Data == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("payOS webhook không đọc được client_ip=%s body=%q", c.ClientIP(), string(body)))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	if !payOSVerifyData(envelope.Data, envelope.Signature) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("payOS webhook sai chữ ký client_ip=%s body=%q", c.ClientIP(), string(body)))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	orderCode, okOrder := payOSNumber(envelope.Data["orderCode"])
	paid, okAmount := payOSNumber(envelope.Data["amount"])
	dataCode, _ := envelope.Data["code"].(string)
	if !okOrder || !okAmount {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("payOS webhook thiếu orderCode/amount client_ip=%s body=%q", c.ClientIP(), string(body)))
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}
	if envelope.Code != "00" || dataCode != "00" {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("payOS webhook bỏ qua giao dịch không thành công order_code=%d code=%s data_code=%s", orderCode, envelope.Code, dataCode))
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}
	_ = completePayOSOrder(c.Request.Context(), orderCode, paid, c.ClientIP(), "webhook")
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// PayOSReturn: trình duyệt quay về từ trang payOS. Không tin tham số trên URL -
// gateway tự hỏi payOS trạng thái đơn, trả đủ thì cộng, rồi chuyển về trang ví.
func PayOSReturn(c *gin.Context) {
	target := paymentReturnPath("/wallet?show_history=true")
	orderCode, err := strconv.ParseInt(c.Query("orderCode"), 10, 64)
	if err != nil || orderCode <= 0 || orderCode > payOSMaxOrderCode || !isPayOSTopUpEnabled() {
		c.Redirect(http.StatusFound, target)
		return
	}
	topUp := model.GetTopUpByTradeNo(payOSTradeNo(orderCode))
	if topUp == nil || topUp.PaymentProvider != model.PaymentProviderPayOS || topUp.Status != common.TopUpStatusPending {
		c.Redirect(http.StatusFound, target)
		return
	}
	envelope, err := payOSRequest(c.Request.Context(), http.MethodGet, "/v2/payment-requests/"+strconv.FormatInt(orderCode, 10), nil)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("payOS return: hỏi trạng thái đơn thất bại order_code=%d error=%q", orderCode, err.Error()))
		c.Redirect(http.StatusFound, target)
		return
	}
	paid, _ := payOSNumber(envelope.Data["amountPaid"])
	remaining, okRemaining := payOSNumber(envelope.Data["amountRemaining"])
	status, _ := envelope.Data["status"].(string)
	if paid > 0 && okRemaining && remaining <= 0 {
		_ = completePayOSOrder(c.Request.Context(), orderCode, paid, c.ClientIP(), "return")
	} else if status == "CANCELLED" || status == "EXPIRED" {
		_ = model.UpdatePendingTopUpStatus(payOSTradeNo(orderCode), model.PaymentProviderPayOS, common.TopUpStatusExpired)
	}
	c.Redirect(http.StatusFound, target)
}
