package helpers

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"time"

	"github.com/resend/resend-go/v3"
)

// SendOTPEmail sends an OTP code to the user.
//
// Hosted platforms often block outbound SMTP (ports 465/587). If RESEND_API_KEY
// is set, we prefer sending via Resend over HTTPS. Otherwise we try SMTP.
//
// SMTP requires SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, SMTP_FROM.
// Also supports legacy names SMTP_EMAIL and SMTP_APP_PASSWORD.
func SendOTPEmail(toEmail, otp, purpose string) error {
	if apiKey := os.Getenv("RESEND_API_KEY"); apiKey != "" {
		from := os.Getenv("RESEND_FROM")
		if from == "" {
			from = "Pine <onboarding@resend.dev>"
		}
		return sendOTPResend(apiKey, from, toEmail, otp, purpose)
	}

	host := os.Getenv("SMTP_HOST")
	portStr := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	from := os.Getenv("SMTP_FROM")

	// Legacy env names fallback
	if user == "" {
		user = os.Getenv("SMTP_EMAIL")
	}
	if pass == "" {
		pass = os.Getenv("SMTP_APP_PASSWORD")
	}
	if from == "" && user != "" {
		from = user
	}

	if host == "" || portStr == "" || user == "" || pass == "" || from == "" {
		log.Printf("email: SMTP not configured, OTP for %s is %s", toEmail, otp)
		return nil // Graceful fallback - don't break the flow
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		log.Printf("email: invalid SMTP_PORT %q", portStr)
		return fmt.Errorf("invalid SMTP_PORT")
	}

	subject, htmlBody := buildOTPEmail(purpose, otp)
	msgBytes := buildSMTPMessage(from, toEmail, subject, htmlBody)

	fromEnvelope := from
	if a, err := mail.ParseAddress(from); err == nil && a.Address != "" {
		fromEnvelope = a.Address
	}

	if err := sendMailSMTP(host, port, user, pass, fromEnvelope, toEmail, msgBytes); err != nil {
		return fmt.Errorf("smtp send error: %w", err)
	}

	log.Printf("email: OTP sent to %s via SMTP", toEmail)
	return nil
}

// LogSMTPConfig logs whether an email provider is configured at startup without exposing secrets.
func LogSMTPConfig() {
	if apiKey := os.Getenv("RESEND_API_KEY"); apiKey != "" {
		from := os.Getenv("RESEND_FROM")
		if from == "" {
			from = "Pine <onboarding@resend.dev>"
		}
		log.Printf("email: provider=resend configured (from=%s)", from)
		return
	}

	host := os.Getenv("SMTP_HOST")
	portStr := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	from := os.Getenv("SMTP_FROM")

	// Legacy env names fallback
	if user == "" {
		user = os.Getenv("SMTP_EMAIL")
	}
	if pass == "" {
		pass = os.Getenv("SMTP_APP_PASSWORD")
	}
	if from == "" && user != "" {
		from = user
	}

	missing := []string{}
	if host == "" {
		missing = append(missing, "SMTP_HOST")
	}
	if portStr == "" {
		missing = append(missing, "SMTP_PORT")
	}
	if user == "" {
		missing = append(missing, "SMTP_USER/SMTP_EMAIL")
	}
	if pass == "" {
		missing = append(missing, "SMTP_PASS/SMTP_APP_PASSWORD")
	}
	if from == "" {
		missing = append(missing, "SMTP_FROM")
	}

	if len(missing) > 0 {
		log.Printf("email: SMTP not fully configured (missing: %v)", missing)
		return
	}

	log.Printf("email: provider=smtp configured (host=%s port=%s from=%s)", host, portStr, from)
}

func buildOTPEmail(purpose, otp string) (string, string) {
	subject := "Your Pine verification code"
	heading := "Verify your email"
	subtext := "Use the code below to verify your Pine account."

	switch purpose {
	case "signup":
		subject = "Welcome to Pine - verify your email"
		heading = "Welcome to Pine"
		subtext = "You're almost there. Use this code to verify your email address."
	case "login":
		subject = "Pine - verify your identity"
		heading = "Verify your identity"
		subtext = "We noticed your account hasn't been verified yet. Enter this code to continue."
	case "reset":
		subject = "Pine - password reset code"
		heading = "Password reset"
		subtext = "Use this code to reset your password. It expires in 10 minutes."
	}

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
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
          <p style="margin:0;font-size:11px;color:#94a3b8;">Pine - your calm, personal journal</p>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, heading, subtext, otp)

	return subject, htmlBody
}

func buildSMTPMessage(fromHeader, toEmail, subject, htmlBody string) []byte {
	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s\r\n", fromHeader))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", toEmail))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)
	return msg.Bytes()
}

func sendOTPResend(apiKey, from, toEmail, otp, purpose string) error {
	subject, htmlBody := buildOTPEmail(purpose, otp)

	client := resend.NewClient(apiKey)
	params := &resend.SendEmailRequest{
		From:    from,
		To:      []string{toEmail},
		Subject: subject,
		Html:    htmlBody,
	}

	sent, err := client.Emails.Send(params)
	if err != nil {
		return err
	}

	log.Printf("email: OTP sent to %s via Resend (id: %s)", toEmail, sent.Id)
	return nil
}

func sendMailSMTP(host string, port int, user, pass, fromEnvelope, toEmail string, msg []byte) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	auth := smtp.PlainAuth("", user, pass, host)

	dialer := net.Dialer{Timeout: 10 * time.Second}

	var conn net.Conn
	var err error
	if port == 465 {
		tlsConfig := &tls.Config{ServerName: host}
		conn, err = tls.DialWithDialer(&dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()

	if port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{ServerName: host}
			if err := c.StartTLS(tlsConfig); err != nil {
				return err
			}
		} else if port == 587 {
			return fmt.Errorf("server does not support STARTTLS")
		}
	}

	if err := c.Auth(auth); err != nil {
		return err
	}
	if err := c.Mail(fromEnvelope); err != nil {
		return err
	}
	if err := c.Rcpt(toEmail); err != nil {
		return err
	}

	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	return c.Quit()
}
