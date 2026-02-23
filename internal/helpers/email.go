package helpers

import (
	"fmt"
	"log"
	"net/smtp"
	"os"
)

// SendOTPEmail sends an OTP code to the user via Gmail SMTP.
// Requires SMTP_EMAIL and SMTP_APP_PASSWORD env vars.
func SendOTPEmail(toEmail, otp, purpose string) error {
	from := os.Getenv("SMTP_EMAIL")
	password := os.Getenv("SMTP_APP_PASSWORD")

	if from == "" || password == "" {
		log.Printf("email: SMTP not configured, OTP for %s is %s", toEmail, otp)
		return nil // Graceful fallback — don't break the flow
	}

	smtpHost := "smtp.gmail.com"
	smtpPort := "587"

	// Subject line based on purpose
	subject := "Your Pine verification code"
	heading := "Verify your email"
	subtext := "Use the code below to verify your Pine account."

	switch purpose {
	case "signup":
		subject = "Welcome to Pine — verify your email"
		heading = "Welcome to Pine"
		subtext = "You're almost there. Use this code to verify your email address."
	case "login":
		subject = "Pine — verify your identity"
		heading = "Verify your identity"
		subtext = "We noticed your account hasn't been verified yet. Enter this code to continue."
	case "reset":
		subject = "Pine — password reset code"
		heading = "Password reset"
		subtext = "Use this code to reset your password. It expires in 10 minutes."
	}

	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="margin:0;padding:0;background:#f8faf8;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;">
  <table width="100%%" cellpadding="0" cellspacing="0" style="padding:40px 0;">
    <tr><td align="center">
      <table width="420" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:12px;border:1px solid #e2e8f0;overflow:hidden;">
        <tr><td style="padding:32px 32px 24px;text-align:center;">
          <div style="font-size:24px;margin-bottom:8px;">&#127794;</div>
          <h1 style="margin:0 0 8px;font-size:22px;font-weight:700;color:#0f172a;font-family:Georgia,'Times New Roman',serif;">%s</h1>
          <p style="margin:0 0 24px;font-size:14px;color:#64748b;line-height:1.5;">%s</p>
          <div style="background:#f1f5f9;border-radius:10px;padding:20px;margin:0 auto;display:inline-block;">
            <span style="font-size:32px;letter-spacing:8px;font-weight:700;color:#0f172a;font-family:'Courier New',monospace;">%s</span>
          </div>
          <p style="margin:20px 0 0;font-size:12px;color:#94a3b8;">This code expires in 10 minutes.</p>
        </td></tr>
        <tr><td style="padding:16px 32px;border-top:1px solid #e2e8f0;text-align:center;">
          <p style="margin:0;font-size:11px;color:#94a3b8;">Pine &mdash; your calm, personal journal</p>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, heading, subtext, otp)

	msg := fmt.Sprintf(
		"From: Pine <%s>\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		from, toEmail, subject, body,
	)

	auth := smtp.PlainAuth("", from, password, smtpHost)
	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, from, []string{toEmail}, []byte(msg))
	if err != nil {
		log.Printf("email: failed to send to %s: %v", toEmail, err)
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Printf("email: OTP sent to %s", toEmail)
	return nil
}
