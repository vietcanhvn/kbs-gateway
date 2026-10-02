package common

import (
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmailHTMLToTextKeepsParagraphsAndLinkTargets(t *testing.T) {
	html := `<div><p>Xin chào,</p><p>Mã của bạn: <strong>123456</strong></p>` +
		`<p><a href="https://api.kimbox.studio/wallet">Nạp tiền</a> &middot; <a href="mailto:contact@kimbox.studio">contact@kimbox.studio</a><br>` +
		`<a href="https://api.kimbox.studio/x">https://api.kimbox.studio/x</a></p><hr></div>`
	assert.Equal(t,
		"Xin chào,\n\nMã của bạn: 123456\n\nNạp tiền (https://api.kimbox.studio/wallet) · contact@kimbox.studio\nhttps://api.kimbox.studio/x",
		EmailHTMLToText(html))
}

func TestBuildEmailMIMEIsMultipartWithPlainTextAndTransactionalHeaders(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 30, 0, 0, time.FixedZone("ICT", 7*3600))
	raw, err := buildEmailMIME(EmailMessage{
		Subject: "[KBS API] Mã xác minh email của bạn: 123456",
		To:      "nguoi-dung@example.com",
		HTML:    "<p>Mã của bạn: <strong>123456</strong></p>",
	}, "KBS API", "contact@kimbox.studio", "<1.abc@kimbox.studio>", now)
	require.NoError(t, err)

	message, err := mail.ReadMessage(strings.NewReader(string(raw)))
	require.NoError(t, err)
	from, err := mail.ParseAddress(message.Header.Get("From"))
	require.NoError(t, err)
	assert.Equal(t, "KBS API", from.Name)
	assert.Equal(t, "contact@kimbox.studio", from.Address)
	assert.Equal(t, "contact@kimbox.studio", message.Header.Get("Reply-To"))
	assert.Equal(t, "<1.abc@kimbox.studio>", message.Header.Get("Message-ID"))
	assert.Equal(t, "1.0", message.Header.Get("MIME-Version"))
	assert.Equal(t, "auto-generated", message.Header.Get("Auto-Submitted"))
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	require.NoError(t, err)
	assert.Equal(t, "[KBS API] Mã xác minh email của bạn: 123456", subject)

	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/alternative", mediaType)
	reader := multipart.NewReader(message.Body, params["boundary"])
	var types, bodies []string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		// multipart.Reader tự giải mã quoted-printable; phần nào chưa giải thì giải tay.
		var body []byte
		if part.Header.Get("Content-Transfer-Encoding") == "quoted-printable" {
			body, err = io.ReadAll(quotedprintable.NewReader(part))
		} else {
			body, err = io.ReadAll(part)
		}
		require.NoError(t, err)
		types = append(types, part.Header.Get("Content-Type"))
		bodies = append(bodies, strings.TrimSpace(string(body)))
	}
	// Bản chữ thuần đi trước, HTML đi sau; bản chữ thuần tự suy ra từ HTML khi không đưa.
	assert.Equal(t, []string{"text/plain; charset=UTF-8", "text/html; charset=UTF-8"}, types)
	assert.Equal(t, []string{"Mã của bạn: 123456", "<p>Mã của bạn: <strong>123456</strong></p>"}, bodies)
}

func TestBuildEmailMIMEUsesGivenPlainText(t *testing.T) {
	raw, err := buildEmailMIME(EmailMessage{Subject: "s", To: "a@b.c", HTML: "<p>html</p>", Text: "ban chu thuan"},
		"Hệ thống", "contact@kimbox.studio", "<2@kimbox.studio>", time.Now())
	require.NoError(t, err)
	assert.Contains(t, string(raw), "ban chu thuan")
	// Tên người gửi có dấu được mã hoá đúng chuẩn, không để nguyên UTF-8 trong tiêu đề.
	assert.Contains(t, string(raw), "From: =?UTF-8?q?")
}
