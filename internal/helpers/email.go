package helpers

import (
    "bytes"
    "crypto/tls"
    "fmt"
    "log"
    "net"
    "net/smtp"
    "os"
    "strconv"
)

// SendOTPEmail sends an OTP code to the user via SMTP.
// Requires SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, SMTP_FROM env vars.
// Also supports legacy names SMTP_EMAIL and SMTP_APP_PASSWORD.
func SendOTPEmail(toEmail, otp, purpose string) error {
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

    // Subject line based on purpose
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

    var msg bytes.Buffer
    msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
    msg.WriteString(fmt.Sprintf("To: %s\r\n", toEmail))
    msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
    msg.WriteString("MIME-Version: 1.0\r\n")
    msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
    msg.WriteString("\r\n")
    msg.WriteString(htmlBody)

    addr := net.JoinHostPort(host, strconv.Itoa(port))
    auth := smtp.PlainAuth("", user, pass, host)

    // Prefer STARTTLS when using port 587. Use TLS directly for port 465.
    if port == 465 {
        tlsConfig := &tls.Config{ServerName: host}
        conn, err := tls.Dial("tcp", addr, tlsConfig)
        if err != nil {
            log.Printf("email: tls dial error: %v", err)
            return fmt.Errorf("smtp tls dial error: %w", err)
        }
        c, err := smtp.NewClient(conn, host)
        if err != nil {
            log.Printf("email: smtp client error: %v", err)
            return fmt.Errorf("smtp client error: %w", err)
        }
        defer c.Close()

        if err := c.Auth(auth); err != nil {
            log.Printf("email: smtp auth error: %v", err)
            return fmt.Errorf("smtp auth error: %w", err)
        }
        if err := c.Mail(from); err != nil {
            return fmt.Errorf("smtp from error: %w", err)
        }
        if err := c.Rcpt(toEmail); err != nil {
            return fmt.Errorf("smtp rcpt error: %w", err)
        }
        w, err := c.Data()
        if err != nil {
            return fmt.Errorf("smtp data error: %w", err)
        }
        if _, err := w.Write(msg.Bytes()); err != nil {
            return fmt.Errorf("smtp write error: %w", err)
        }
        if err := w.Close(); err != nil {
            return fmt.Errorf("smtp close error: %w", err)
        }
        if err := c.Quit(); err != nil {
            return fmt.Errorf("smtp quit error: %w", err)
        }
        log.Printf("email: OTP sent to %s via SMTP", toEmail)
        return nil
    }

    if err := smtp.SendMail(addr, auth, from, []string{toEmail}, msg.Bytes()); err != nil {
        log.Printf("email: failed to send to %s: %v", toEmail, err)
        return fmt.Errorf("failed to send email: %w", err)
    }

    log.Printf("email: OTP sent to %s via SMTP", toEmail)
    return nil
}

// LogSMTPConfig logs whether SMTP is configured at startup without exposing secrets.
func LogSMTPConfig() {
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

	log.Printf("email: SMTP configured (host=%s port=%s from=%s)", host, portStr, from)
}
