package utils

import (
	"crypto/tls"
	"fmt"
	"gopkg.in/gomail.v2"
)

type EmailSender struct {
	Host     string
	Port     int
	Sender   string
	Password string
	UseSSL   bool
}

func NewEmailSender(host string, port int, sender, password string, useSSL bool) *EmailSender {
	return &EmailSender{
		Host:     host,
		Port:     port,
		Sender:   sender,
		Password: password,
		UseSSL:   useSSL,
	}
}

func (e *EmailSender) Send(to, subject, html string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", e.Sender)
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
	return fmt.Sprintf(`
    <!DOCTYPE html>
    <html>
    <head>
        <meta charset="utf-8">
        <title>验证码</title>
        <style>
            .container { width: 100%%; max-width: 600px; margin: 0 auto; padding: 20px; font-family: 'Microsoft YaHei', Arial, sans-serif; }
            .header { text-align: center; padding: 20px 0; background: #f8f9fa; border-radius: 5px 5px 0 0; }
            .content { background: white; padding: 30px; border-radius: 0 0 5px 5px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
            .code-container { text-align: center; margin: 30px 0; padding: 20px; background: #f8f9fa; border-radius: 5px; }
            .code { font-size: 32px; font-weight: bold; letter-spacing: 8px; color: #2c3e50; padding: 10px 20px; background: white; border: 2px dashed #3498db; border-radius: 5px; display: inline-block; }
            .footer { margin-top: 20px; padding-top: 20px; border-top: 1px solid #eee; color: #666; font-size: 12px; text-align: center; }
            .warning { color: #e74c3c; font-size: 14px; margin-top: 20px; }
        </style>
    </head>
    <body>
        <div class="container">
            <div class="header">
                <h2 style="color: #2c3e50; margin: 0;">验证码通知</h2>
            </div>
            <div class="content">
                <p>尊敬的用户：</p>
                <p>您好！您正在进行邮箱验证，请在验证码输入框中输入以下验证码：</p>
                <div class="code-container">
                    <div class="code">%s</div>
                </div>
                <p class="warning">⚠️ 验证码有效期为3分钟，请尽快完成验证。</p>
                <p>如果这不是您的操作，请忽略此邮件。</p>
                <div class="footer">
                    <p>此邮件为系统自动发送，请勿回复。</p>
                </div>
            </div>
        </div>
    </body>
    </html>
    `, code)
} 