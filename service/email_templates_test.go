package service

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useKBSBrandForEmailTest(t *testing.T) {
	t.Helper()
	oldName, oldFrom := common.SystemName, common.SMTPFrom
	oldCompany, oldWebsite := common.EmailBrandCompany, common.EmailBrandWebsite
	common.SystemName, common.SMTPFrom = "KBS API", "contact@kimbox.studio"
	common.EmailBrandCompany, common.EmailBrandWebsite = "Kim Box Studio (KMG)", "https://kimbox.studio"
	t.Cleanup(func() {
		common.SystemName, common.SMTPFrom = oldName, oldFrom
		common.EmailBrandCompany, common.EmailBrandWebsite = oldCompany, oldWebsite
	})
}

// In mẫu ra tệp khi cần soát bằng mắt: EMAIL_PREVIEW_DIR=/duong-dan go test ./service -run Email
func writeEmailPreview(t *testing.T, name string, message common.EmailMessage) {
	t.Helper()
	dir := os.Getenv("EMAIL_PREVIEW_DIR")
	if dir == "" {
		return
	}
	text := message.Text
	if text == "" {
		text = common.EmailHTMLToText(message.HTML)
	}
	require.NoError(t, os.WriteFile(dir+"/"+name+".html", []byte(message.HTML), 0o644))
	require.NoError(t, os.WriteFile(dir+"/"+name+".txt", []byte("Subject: "+message.Subject+"\n\n"+text+"\n"), 0o644))
}

func assertBrandedEmail(t *testing.T, message common.EmailMessage) {
	t.Helper()
	assert.True(t, strings.HasPrefix(message.Subject, "[KBS API] "), message.Subject)
	for _, body := range []string{message.HTML, message.Text} {
		if body == "" {
			continue
		}
		assert.Contains(t, body, "KBS API")
		assert.Contains(t, body, "Kim Box Studio (KMG)")
		assert.Contains(t, body, "kimbox.studio")
		assert.Contains(t, body, "contact@kimbox.studio")
	}
	// Không ảnh ngoài, không kịch bản, không chữ mẫu gốc tiếng Trung.
	assert.NotContains(t, message.HTML, "<img")
	assert.NotContains(t, message.HTML, "<script")
	assert.NotRegexp(t, `\p{Han}`, message.HTML+message.Subject+message.Text)
}

func TestVerificationEmailCarriesBrandCodeValidityAndIgnoreNote(t *testing.T) {
	useKBSBrandForEmailTest(t)
	message := VerificationEmail("483920", 10)
	writeEmailPreview(t, "ma-xac-minh", message)

	assertBrandedEmail(t, message)
	assert.Equal(t, "[KBS API] Mã xác minh email của bạn: 483920", message.Subject)
	for _, body := range []string{message.HTML, message.Text} {
		assert.Contains(t, body, "483920")
		assert.Contains(t, body, "10 phút")
		assert.Contains(t, body, "Nếu không phải bạn yêu cầu, hãy bỏ qua thư này")
		assert.Contains(t, body, "valid for 10 minutes")
	}
}

func TestPasswordResetEmailLinksOnlyToTheSystemItself(t *testing.T) {
	useKBSBrandForEmailTest(t)
	link := "https://api.kimbox.studio/user/reset?email=a%40b.c&token=tok123"
	message := PasswordResetEmail(link, 10)
	writeEmailPreview(t, "dat-lai-mat-khau", message)

	assertBrandedEmail(t, message)
	assert.Equal(t, "[KBS API] Đặt lại mật khẩu tài khoản của bạn", message.Subject)
	assert.Contains(t, message.Text, link)
	assert.Contains(t, message.HTML, `href="https://api.kimbox.studio/user/reset?email=a%40b.c&amp;token=tok123"`)
	assert.Contains(t, message.HTML, "mật khẩu của bạn vẫn giữ nguyên")
	// Mọi liên kết http(s) trỏ về hệ thống hoặc trang web của đơn vị vận hành.
	for _, piece := range strings.Split(message.HTML, `href="`)[1:] {
		target := piece[:strings.Index(piece, `"`)]
		assert.True(t,
			strings.HasPrefix(target, "https://api.kimbox.studio/") || target == "https://kimbox.studio" || strings.HasPrefix(target, "mailto:contact@kimbox.studio"),
			target)
	}
}

func TestQuotaWarningEmailNamesTheBalanceAndTheWalletLink(t *testing.T) {
	useKBSBrandForEmailTest(t)
	subject, body := QuotaWarningEmail("$0.12", "https://api.kimbox.studio/wallet", false)
	message := NoticeEmail(subject, body)
	writeEmailPreview(t, "so-du-sap-het", message)

	assertBrandedEmail(t, message)
	assert.Equal(t, "[KBS API] Số dư tài khoản sắp hết: còn $0.12", message.Subject)
	assert.Contains(t, message.HTML, "<strong>$0.12</strong>")
	assert.Contains(t, message.HTML, `href="https://api.kimbox.studio/wallet"`)

	subscriptionSubject, _ := QuotaWarningEmail("$1", "https://api.kimbox.studio/wallet", true)
	assert.Equal(t, "Hạn mức gói đăng ký sắp hết: còn $1", subscriptionSubject)
}

func TestNoticeEmailWrapsPlainNoticesInTheBrandLayout(t *testing.T) {
	useKBSBrandForEmailTest(t)
	message := NoticeEmail("Kênh «Gemini-API» (#2) đã bị tắt tự động", "Kênh «Gemini-API» (#2) đã bị tắt tự động. Lý do: 401")
	writeEmailPreview(t, "thong-bao-quan-tri", message)

	assertBrandedEmail(t, message)
	assert.Contains(t, message.HTML, "<p>Kênh «Gemini-API» (#2) đã bị tắt tự động. Lý do: 401</p>")
	// Tiêu đề đã có tên hệ thống thì không thêm lần nữa.
	assert.Equal(t, message.Subject, NoticeEmail(message.Subject, "x").Subject)
}
