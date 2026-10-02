package common

import (
	"bytes"
	"fmt"
	"html"
	"mime"
	"mime/quotedprintable"
	"regexp"
	"strings"
	"time"
)

// EmailMessage là một thư hệ thống. Text để trống thì bản chữ thuần được suy ra
// từ HTML: thư chỉ có HTML mà không có bản text/plain dễ bị bộ lọc coi là thư
// hàng loạt.
type EmailMessage struct {
	Subject string
	To      string
	HTML    string
	Text    string
}

var (
	emailLinkPattern       = regexp.MustCompile(`(?is)<a\s[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	emailParagraphPattern  = regexp.MustCompile(`(?i)</p>|</div>|</h[1-6]>|<hr[^>]*>`)
	emailLineBreakPattern  = regexp.MustCompile(`(?i)<br\s*/?>|</tr>|</li>`)
	emailTagPattern        = regexp.MustCompile(`(?s)<[^>]+>`)
	emailBlankLinesPattern = regexp.MustCompile(`\n[ \t]*\n[ \t\n]*`)
)

// EmailHTMLToText đổi HTML của thư thành chữ thuần: giữ xuống dòng theo đoạn,
// liên kết thành "chữ (địa chỉ)".
func EmailHTMLToText(content string) string {
	text := emailLinkPattern.ReplaceAllStringFunc(content, func(link string) string {
		parts := emailLinkPattern.FindStringSubmatch(link)
		href, label := parts[1], strings.TrimSpace(emailTagPattern.ReplaceAllString(parts[2], ""))
		href = strings.TrimPrefix(href, "mailto:")
		if label == "" || label == href {
			return href
		}
		return fmt.Sprintf("%s (%s)", label, href)
	})
	// Xuống dòng trong mã HTML không có nghĩa; chỉ thẻ mới tạo dòng mới.
	text = strings.Join(strings.Fields(text), " ")
	text = emailParagraphPattern.ReplaceAllString(text, "\n\n")
	text = emailLineBreakPattern.ReplaceAllString(text, "\n")
	text = emailTagPattern.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	text = emailBlankLinesPattern.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	return strings.TrimSpace(text)
}

// buildEmailMIME dựng thư multipart/alternative (text/plain + text/html), có đủ
// các tiêu đề mà hộp thư lớn mong ở một thư giao dịch: tên người gửi, Reply-To,
// Message-ID theo tên miền gửi, MIME-Version, Auto-Submitted.
func buildEmailMIME(message EmailMessage, fromName string, fromAddress string, messageID string, now time.Time) ([]byte, error) {
	text := message.Text
	if strings.TrimSpace(text) == "" {
		text = EmailHTMLToText(message.HTML)
	}
	boundary := "kbs-" + GetRandomString(24)

	var mail bytes.Buffer
	header := func(name string, value string) {
		mail.WriteString(name + ": " + value + "\r\n")
	}
	header("To", message.To)
	header("From", fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("UTF-8", fromName), fromAddress))
	header("Reply-To", fromAddress)
	header("Subject", mime.BEncoding.Encode("UTF-8", message.Subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", messageID)
	header("MIME-Version", "1.0")
	header("Auto-Submitted", "auto-generated")
	header("Content-Type", fmt.Sprintf("multipart/alternative; boundary=\"%s\"", boundary))
	mail.WriteString("\r\n")

	// Bản chữ thuần trước, HTML sau: trình đọc thư chọn phần cuối cùng nó hiểu.
	for _, part := range []struct{ contentType, body string }{
		{"text/plain; charset=UTF-8", text},
		{"text/html; charset=UTF-8", message.HTML},
	} {
		mail.WriteString("--" + boundary + "\r\n")
		header("Content-Type", part.contentType)
		header("Content-Transfer-Encoding", "quoted-printable")
		mail.WriteString("\r\n")
		writer := quotedprintable.NewWriter(&mail)
		if _, err := writer.Write([]byte(part.body)); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		mail.WriteString("\r\n")
	}
	mail.WriteString("--" + boundary + "--\r\n")
	return mail.Bytes(), nil
}
