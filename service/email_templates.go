package service

import (
	"fmt"
	"html"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Mẫu email hệ thống, song ngữ Việt - Anh, mang tên hệ thống và đơn vị vận hành.
//
// Mẫu gốc của phần mềm nguồn mở là vài câu ngắn giống hệt nhau ở mọi nơi cài
// nó, nên hộp thư lớn dễ xếp vào Spam. Mỗi thư ở đây nói rõ: thư của ai, vì sao
// người nhận có thư này, phải làm gì, hiệu lực bao lâu, và liên hệ ở đâu. Mọi
// liên kết trỏ về chính địa chỉ của hệ thống; không ảnh ngoài, không link rút gọn.

const emailWrapperStyle = "font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1f2937;font-size:15px;line-height:1.6;max-width:520px;margin:0 auto;padding:24px"

// emailSubject thêm tên hệ thống vào đầu tiêu đề: "[KBS API] ...".
func emailSubject(subject string) string {
	prefix := "[" + common.SystemName + "] "
	if strings.HasPrefix(subject, prefix) {
		return subject
	}
	return prefix + subject
}

// emailWebsiteLabel bỏ "https://" để hiện gọn: "kimbox.studio".
func emailWebsiteLabel(website string) string {
	label := strings.TrimPrefix(strings.TrimPrefix(website, "https://"), "http://")
	return strings.TrimRight(label, "/")
}

// renderEmailLayout bọc phần thân (HTML) vào khung chung: tên hệ thống ở đầu,
// chân thư có đơn vị vận hành, trang web và email hỗ trợ.
func renderEmailLayout(bodyHTML string) string {
	name := html.EscapeString(common.SystemName)
	company := html.EscapeString(common.EmailBrandCompany)
	website := html.EscapeString(common.EmailBrandWebsite)
	contact := html.EscapeString(common.SMTPFrom)
	var b strings.Builder
	b.WriteString("<div style=\"" + emailWrapperStyle + "\">\n")
	b.WriteString("<p style=\"font-size:18px;font-weight:700;margin:0 0 16px\">" + name + "</p>\n")
	b.WriteString(bodyHTML)
	b.WriteString("\n<hr style=\"border:none;border-top:1px solid #e5e7eb;margin:24px 0 12px\">\n")
	b.WriteString("<p style=\"font-size:12px;color:#6b7280;margin:0\">")
	b.WriteString("Thư tự động từ " + name + ", dịch vụ của " + company + ". / Automated message from " + name + ", a service of " + company + ".<br>\n")
	b.WriteString("Website: <a href=\"" + website + "\">" + html.EscapeString(emailWebsiteLabel(common.EmailBrandWebsite)) + "</a>")
	if contact != "" {
		b.WriteString(" &middot; Hỗ trợ / Support: <a href=\"mailto:" + contact + "\">" + contact + "</a>")
	}
	b.WriteString("</p>\n</div>")
	return b.String()
}

// emailTextFooter là chân thư của bản chữ thuần.
func emailTextFooter() string {
	footer := fmt.Sprintf("--\nThư tự động từ %s, dịch vụ của %s. / Automated message from %s, a service of %s.\nWebsite: %s",
		common.SystemName, common.EmailBrandCompany, common.SystemName, common.EmailBrandCompany, common.EmailBrandWebsite)
	if common.SMTPFrom != "" {
		footer += "\nHỗ trợ / Support: " + common.SMTPFrom
	}
	return footer
}

// VerificationEmail: thư mã xác minh email (đăng ký hoặc liên kết email).
func VerificationEmail(code string, validMinutes int) common.EmailMessage {
	name := common.SystemName
	body := fmt.Sprintf(`<p>Xin chào,</p>
<p>Địa chỉ email này vừa được nhập để đăng ký hoặc liên kết với một tài khoản %[1]s. Hãy nhập mã dưới đây vào trang %[1]s để xác minh email:</p>
<p style="font-size:28px;font-weight:700;letter-spacing:4px;margin:16px 0">%[2]s</p>
<p>Mã có hiệu lực trong %[3]d phút. Nếu không phải bạn yêu cầu, hãy bỏ qua thư này; không có gì thay đổi với email của bạn.</p>
<p style="color:#6b7280;margin-top:20px">Hello, this email address was entered to sign up for or link to a %[1]s account. Enter the code <strong>%[2]s</strong> on the %[1]s page to verify it. The code is valid for %[3]d minutes. If you did not request it, you can ignore this email.</p>`,
		html.EscapeString(name), html.EscapeString(code), validMinutes)
	text := fmt.Sprintf(`Xin chào,

Địa chỉ email này vừa được nhập để đăng ký hoặc liên kết với một tài khoản %[1]s. Hãy nhập mã dưới đây vào trang %[1]s để xác minh email:

%[2]s

Mã có hiệu lực trong %[3]d phút. Nếu không phải bạn yêu cầu, hãy bỏ qua thư này; không có gì thay đổi với email của bạn.

Hello, this email address was entered to sign up for or link to a %[1]s account. Enter the code %[2]s on the %[1]s page to verify it. The code is valid for %[3]d minutes. If you did not request it, you can ignore this email.

%[4]s`, name, code, validMinutes, emailTextFooter())
	return common.EmailMessage{
		Subject: emailSubject(fmt.Sprintf("Mã xác minh email của bạn: %s", code)),
		HTML:    renderEmailLayout(body),
		Text:    text,
	}
}

// PasswordResetEmail: thư đặt lại mật khẩu, link trỏ về chính hệ thống.
func PasswordResetEmail(link string, validMinutes int) common.EmailMessage {
	name := common.SystemName
	safeLink := html.EscapeString(link)
	body := fmt.Sprintf(`<p>Xin chào,</p>
<p>Chúng tôi nhận được yêu cầu đặt lại mật khẩu cho tài khoản %[1]s gắn với email này. Bấm nút dưới đây để đặt mật khẩu mới:</p>
<p style="margin:20px 0"><a href="%[2]s" style="display:inline-block;background:#0f766e;color:#ffffff;text-decoration:none;padding:10px 18px;border-radius:6px;font-weight:600">Đặt lại mật khẩu</a></p>
<p>Nếu nút không bấm được, hãy chép địa chỉ sau vào trình duyệt:<br><a href="%[2]s">%[2]s</a></p>
<p>Liên kết có hiệu lực trong %[3]d phút. Nếu không phải bạn yêu cầu, hãy bỏ qua thư này; mật khẩu của bạn vẫn giữ nguyên.</p>
<p style="color:#6b7280;margin-top:20px">Hello, we received a request to reset the password of the %[1]s account linked to this email. Open the link above to set a new password. It is valid for %[3]d minutes. If you did not request it, ignore this email and your password stays the same.</p>`,
		html.EscapeString(name), safeLink, validMinutes)
	text := fmt.Sprintf(`Xin chào,

Chúng tôi nhận được yêu cầu đặt lại mật khẩu cho tài khoản %[1]s gắn với email này. Mở địa chỉ sau để đặt mật khẩu mới:

%[2]s

Liên kết có hiệu lực trong %[3]d phút. Nếu không phải bạn yêu cầu, hãy bỏ qua thư này; mật khẩu của bạn vẫn giữ nguyên.

Hello, we received a request to reset the password of the %[1]s account linked to this email. Open the link above to set a new password. It is valid for %[3]d minutes. If you did not request it, ignore this email and your password stays the same.

%[4]s`, name, link, validMinutes, emailTextFooter())
	return common.EmailMessage{
		Subject: emailSubject("Đặt lại mật khẩu tài khoản của bạn"),
		HTML:    renderEmailLayout(body),
		Text:    text,
	}
}

// QuotaWarningEmail: thư báo số dư (hoặc hạn mức gói đăng ký) sắp hết.
func QuotaWarningEmail(remaining string, topUpLink string, subscription bool) (subject string, bodyHTML string) {
	what, whatEN := "Số dư tài khoản", "Your balance"
	if subscription {
		what, whatEN = "Hạn mức gói đăng ký", "Your subscription quota"
	}
	name := html.EscapeString(common.SystemName)
	safeLink := html.EscapeString(topUpLink)
	subject = fmt.Sprintf("%s sắp hết: còn %s", what, remaining)
	bodyHTML = fmt.Sprintf(`<p>Xin chào,</p>
<p>%[1]s %[2]s của bạn còn <strong>%[3]s</strong>, thấp hơn mức cảnh báo bạn đã đặt. Để các lệnh gọi API không bị gián đoạn, bạn có thể nạp thêm tại trang Ví:</p>
<p><a href="%[4]s">%[4]s</a></p>
<p>Muốn đổi mức cảnh báo hoặc cách nhận thông báo, vào Hồ sơ trong %[2]s.</p>
<p style="color:#6b7280;margin-top:20px">%[5]s on %[2]s is down to <strong>%[3]s</strong>, below your warning level. Top up on the Wallet page (link above) to keep your API calls running. You can change the warning level or how you are notified in your Profile.</p>`,
		what, name, html.EscapeString(remaining), safeLink, whatEN)
	return subject, bodyHTML
}

// NoticeEmail bọc một thông báo bất kỳ (nội dung HTML ngắn) vào khung thư chung.
// Bản chữ thuần được suy ra từ HTML.
func NoticeEmail(title string, bodyHTML string) common.EmailMessage {
	if !strings.Contains(bodyHTML, "<p") {
		bodyHTML = "<p>" + bodyHTML + "</p>"
	}
	return common.EmailMessage{
		Subject: emailSubject(title),
		HTML:    renderEmailLayout(bodyHTML),
	}
}
