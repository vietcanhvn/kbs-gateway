package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPayOSTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))

	ps := operation_setting.GetPaymentSetting()
	previousConfirmed, previousVersion := ps.ComplianceConfirmed, ps.ComplianceTermsVersion
	ps.ComplianceConfirmed, ps.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion

	previous := []any{setting.PayOSEnabled, setting.PayOSClientId, setting.PayOSApiKey, setting.PayOSChecksumKey, setting.PayOSUnitPrice, payOSAPIBase}
	setting.PayOSEnabled = true
	setting.PayOSClientId = "client"
	setting.PayOSApiKey = "api"
	setting.PayOSChecksumKey = "checksum-secret"
	setting.PayOSUnitPrice = 26000

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMain, previousLog)
		ps.ComplianceConfirmed, ps.ComplianceTermsVersion = previousConfirmed, previousVersion
		setting.PayOSEnabled = previous[0].(bool)
		setting.PayOSClientId = previous[1].(string)
		setting.PayOSApiKey = previous[2].(string)
		setting.PayOSChecksumKey = previous[3].(string)
		setting.PayOSUnitPrice = previous[4].(float64)
		payOSAPIBase = previous[5].(string)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func insertPayOSOrder(t *testing.T, db *gorm.DB, orderCode int64, moneyVND float64) {
	t.Helper()
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "payos_user", Status: common.UserStatusEnabled, Quota: 0}).Error)
	require.NoError(t, (&model.TopUp{
		UserId:          7,
		Amount:          10,
		Money:           moneyVND,
		TradeNo:         payOSTradeNo(orderCode),
		PaymentMethod:   model.PaymentMethodPayOS,
		PaymentProvider: model.PaymentProviderPayOS,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}).Insert())
}

// signedWebhook dựng body webhook giống payOS: ký object data bằng checksum key.
func signedWebhook(t *testing.T, data map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, decodePayOSJSON(raw, &decoded))
	body, err := json.Marshal(map[string]any{
		"code":      "00",
		"desc":      "success",
		"success":   true,
		"data":      json.RawMessage(raw),
		"signature": payOSSign(payOSDataString(decoded)),
	})
	require.NoError(t, err)
	return string(body)
}

func postPayOSWebhook(body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/payos/webhook", strings.NewReader(body))
	PayOSWebhook(ctx)
	return recorder
}

func userQuota(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var user model.User
	require.NoError(t, db.First(&user, 7).Error)
	return user.Quota
}

func paidData(orderCode int64, amount int64) map[string]any {
	return map[string]any{
		"orderCode":              orderCode,
		"amount":                 amount,
		"description":            "CSV3ZX5L NAPTIEN",
		"accountNumber":          "12345678",
		"reference":              "TF230204212323",
		"transactionDateTime":    "2026-09-28 18:25:00",
		"currency":               "VND",
		"paymentLinkId":          "124c33293c934a85be5b7f8761a27a07",
		"code":                   "00",
		"desc":                   "success",
		"counterAccountBankId":   "",
		"counterAccountBankName": "",
		"counterAccountName":     nil,
		"counterAccountNumber":   nil,
		"virtualAccountName":     "",
		"virtualAccountNumber":   "",
	}
}

func TestPayOSDataStringFollowsPayOSRules(t *testing.T) {
	var data map[string]any
	require.NoError(t, decodePayOSJSON([]byte(`{"orderCode":1790000000000123,"amount":260000,"desc":"ok","b":null,"a":true,"items":[{"quantity":1,"name":"x"}]}`), &data))
	// Khoá sắp theo chữ cái, null -> rỗng, số giữ nguyên (không thành 1.79e+15), mảng -> JSON đã sắp khoá.
	require.Equal(t,
		`a=true&amount=260000&b=&desc=ok&items=[{"name":"x","quantity":1}]&orderCode=1790000000000123`,
		payOSDataString(data))
}

func TestPayOSCreateSignatureFormat(t *testing.T) {
	setupPayOSTest(t)
	got := payOSCreateSignature(260000, "https://x/api/payos/return", "NAPTIEN", 123, "https://x/api/payos/return")
	require.Equal(t, payOSSign("amount=260000&cancelUrl=https://x/api/payos/return&description=NAPTIEN&orderCode=123&returnUrl=https://x/api/payos/return"), got)
}

func TestPayOSWebhookCreditsOnceAndIgnoresReplays(t *testing.T) {
	db := setupPayOSTest(t)
	insertPayOSOrder(t, db, 1790000000000123, 260000)

	body := signedWebhook(t, paidData(1790000000000123, 260000))
	require.Equal(t, http.StatusOK, postPayOSWebhook(body).Code)
	credited := userQuota(t, db)
	require.Equal(t, int(10*common.QuotaPerUnit), credited)

	// payOS gửi lại cùng thông báo: không cộng lần hai.
	require.Equal(t, http.StatusOK, postPayOSWebhook(body).Code)
	require.Equal(t, credited, userQuota(t, db))

	topUp := model.GetTopUpByTradeNo(payOSTradeNo(1790000000000123))
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
}

func TestPayOSWebhookRejectsBadSignature(t *testing.T) {
	db := setupPayOSTest(t)
	insertPayOSOrder(t, db, 1790000000000124, 260000)

	body := signedWebhook(t, paidData(1790000000000124, 260000))
	// Sửa số tiền sau khi ký: chữ ký không còn khớp.
	tampered := strings.Replace(body, `"amount":260000`, `"amount":999999`, 1)
	require.Equal(t, http.StatusBadRequest, postPayOSWebhook(tampered).Code)
	require.Equal(t, 0, userQuota(t, db))
}

func TestPayOSWebhookRefusesUnderpayment(t *testing.T) {
	db := setupPayOSTest(t)
	insertPayOSOrder(t, db, 1790000000000125, 260000)

	require.Equal(t, http.StatusOK, postPayOSWebhook(signedWebhook(t, paidData(1790000000000125, 100000))).Code)
	require.Equal(t, 0, userQuota(t, db))
	require.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(payOSTradeNo(1790000000000125)).Status)
}

func TestPayOSWebhookAcceptsConfirmationPing(t *testing.T) {
	setupPayOSTest(t)
	// Lúc lưu địa chỉ webhook, payOS gửi thử orderCode 123 không có trong hệ thống: phải trả 2xx.
	require.Equal(t, http.StatusOK, postPayOSWebhook(signedWebhook(t, paidData(123, 3000))).Code)
}

func TestPayOSReturnChecksStatusWithPayOS(t *testing.T) {
	db := setupPayOSTest(t)
	insertPayOSOrder(t, db, 1790000000000126, 260000)

	payOS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/payment-requests/1790000000000126", r.URL.Path)
		require.Equal(t, "client", r.Header.Get("x-client-id"))
		_, _ = w.Write([]byte(`{"code":"00","desc":"success","data":{"orderCode":1790000000000126,"amount":260000,"amountPaid":260000,"amountRemaining":0,"status":"PAID"}}`))
	}))
	defer payOS.Close()
	payOSAPIBase = payOS.URL

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	// Tham số trên URL không được tin: dù ghi "CANCELLED", gateway vẫn hỏi payOS.
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/payos/return?orderCode=1790000000000126&status=CANCELLED", nil)
	PayOSReturn(ctx)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, int(10*common.QuotaPerUnit), userQuota(t, db))
}

func TestGetPayOSPayMoneyRoundsUpToWholeDong(t *testing.T) {
	setupPayOSTest(t)
	setting.PayOSUnitPrice = 26000.5
	require.Equal(t, int64(260005), getPayOSPayMoney(10, "default"))
}
