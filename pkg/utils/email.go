package utils

import (
	"crypto/tls"
	"fmt"
	"gopkg.in/gomail.v2"
	"strings"
)

type EmailSender struct {
	Host        string
	Port        int
	Sender      string
	DisplayName string
	Password    string
	UseSSL      bool
}

func NewEmailSender(host string, port int, sender, displayName, password string, useSSL bool) *EmailSender {
	return &EmailSender{
		Host:        host,
		Port:        port,
		Sender:      sender,
		DisplayName: displayName,
		Password:    password,
		UseSSL:      useSSL,
	}
}

func (e *EmailSender) Send(to, subject, html string) error {
	m := gomail.NewMessage()
	m.SetAddressHeader("From", e.Sender, e.DisplayName)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", html)

	d := gomail.NewDialer(e.Host, e.Port, e.Sender, e.Password)
	if e.UseSSL {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return d.DialAndSend(m)
}

func GetEmailTemplate(code string) string {
	if len(code) != 6 {
		code = fmt.Sprintf("%06s", code)
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>邮箱验证码</title>
<style>
  body { margin:0; padding:20px; background:#f4f4f7; font-family:"Microsoft YaHei",Arial,sans-serif; color:#333; }
  .container { max-width:600px; margin:0 auto; background:#fff; border-radius:8px; box-shadow:0 2px 6px rgba(0,0,0,0.1); }
  .header { background:#4a90e2; padding:30px; text-align:center; color:#fff; border-top-left-radius:8px; border-top-right-radius:8px; }
  .header h1 { margin:0; font-size:24px; }
  .body { padding:30px; font-size:16px; line-height:1.6; text-align:left; }
  .otp-box { text-align:center; margin:25px 0; }
  .otp-digit {
    display:inline-block; width:40px; height:50px; margin:0 5px;
    line-height:50px; font-size:28px; font-weight:bold;
    color:#2c3e50; background:#f0f4f8;
    border:2px solid #4a90e2; border-radius:4px;
  }
  .note { color:#e74c3c; font-size:14px; margin-top:15px; }
  .footer { background:#fafafa; padding:20px; font-size:13px; color:#888; text-align:center;
    border-bottom-left-radius:8px; border-bottom-right-radius:8px; }
  @media (max-width:480px) {
    .otp-digit { width:32px; height:40px; line-height:40px; font-size:24px; margin:0 3px; }
    .header h1 { font-size:20px; }
  }
</style>
</head>
<body>
  <div class="container">
    <div class="header"><h1>邮箱验证码</h1></div>
    <div class="body">
      <p>尊敬的用户：</p>
      <p>您好！您正在进行邮箱验证，请在对应页面输入以下验证码：</p>
      <div class="otp-box">%s</div>
      <p class="note">⚠️ 此验证码有效期为 3 分钟，请尽快使用。</p>
      <p>如非本人操作，请忽略此邮件。</p>
    </div>
    <div class="footer">此邮件由系统自动发送，请勿回复。</div>
  </div>
</body>
</html>`, generateBoxes(code))
}

func generateBoxes(code string) string {
	var sb strings.Builder
	for _, r := range code {
		sb.WriteString(fmt.Sprintf(`<span class="otp-digit">%c</span>`, r))
	}
	return sb.String()
}
