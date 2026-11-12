package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

// KimKid Studio (kimkid.studio) dùng chung kênh payOS với KBS (TL-CEO chốt
// phương án B, 12/11/2026). Kênh payOS chỉ có một webhook, trỏ về đây; đơn KimKid
// có mã 14 chữ số bắt đầu "77" (đơn KBS 16 chữ số) nên gateway chuyển nguyên văn
// thân webhook sang kimkid.studio - không ký lại, không ghi CSDL KBS, không gửi thư.
// kimkid.studio tự kiểm chữ ký và xử lý lặp an toàn.
var kimkidPayOSOrderCodeRe = regexp.MustCompile(`^77\d{12}$`)

const (
	kimkidPayOSWebhookURL     = "https://kimkid.studio/xl/payos/webhook"
	kimkidPayOSForwardTimeout = 15 * time.Second
)

var kimkidPayOSClient = &http.Client{Timeout: kimkidPayOSForwardTimeout}

// kimkidPayOSOrderCode trả mã đơn (dạng chữ) nếu thân webhook là đơn KimKid, "" nếu không.
func kimkidPayOSOrderCode(body []byte) string {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := decodePayOSJSON(body, &envelope); err != nil || envelope.Data == nil {
		return ""
	}
	var code string
	switch v := envelope.Data["orderCode"].(type) {
	case json.Number:
		code = v.String()
	case string:
		code = v
	case float64:
		code = strconv.FormatFloat(v, 'f', -1, 64)
	}
	if kimkidPayOSOrderCodeRe.MatchString(code) {
		return code
	}
	return ""
}

// forwardKimKidPayOSWebhook gửi nguyên văn thân webhook sang kimkid.studio và trả
// về payOS đúng mã + thân kimkid trả. Lỗi mạng/quá giờ -> 502 để payOS gửi lại.
func forwardKimKidPayOSWebhook(c *gin.Context, body []byte, orderCode string) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), kimkidPayOSForwardTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, kimkidPayOSWebhookURL, bytes.NewReader(body))
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("payOS kimkid chuyển tiếp lỗi order_code=%s error=%q", orderCode, err.Error()))
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := kimkidPayOSClient.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("payOS kimkid chuyển tiếp lỗi order_code=%s error=%q", orderCode, err.Error()))
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("payOS kimkid chuyển tiếp đọc trả lời lỗi order_code=%s status=%d error=%q", orderCode, resp.StatusCode, err.Error()))
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("payOS kimkid chuyển tiếp order_code=%s status=%d", orderCode, resp.StatusCode))
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(resp.StatusCode, contentType, respBody)
}
