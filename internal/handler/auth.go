package handler

import (
	"context"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/satyamraj1643/pine_backend_v2/internal/db"
	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
)

var emailRe = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// ─── POST /signup ────────────────────────────────────────

func Signup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email      string `json:"email"`
		Name       string `json:"name"`
		Password   string `json:"password"`
		RePassword string `json:"re_password"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = strings.TrimSpace(req.Name)

	// Validate
	if !emailRe.MatchString(req.Email) {
		helpers.Error(w, http.StatusBadRequest, "invalid email format")
		return
	}
	if len(req.Password) < 8 {
		helpers.Error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if req.Password != req.RePassword {
		helpers.Error(w, http.StatusBadRequest, "passwords do not match")
		return
	}

	// Check if email already exists
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var exists bool
	err := db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`, req.Email,
	).Scan(&exists)
	if err != nil {
		log.Printf("signup: db check error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if exists {
		helpers.Error(w, http.StatusConflict, "email already registered")
		return
	}

	// Hash password
	hash, err := helpers.HashPassword(req.Password)
	if err != nil {
		log.Printf("signup: hash error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Generate OTP
	otp := helpers.GenerateOTP()
	otpExpires := time.Now().Add(10 * time.Minute)
	log.Printf("signup: OTP for %s is %s", req.Email, otp)

	// Insert user
	var userID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO users (email, name, password_hash, is_verified, otp_code, otp_expires)
		 VALUES ($1, $2, $3, false, $4, $5)
		 RETURNING id`,
		req.Email, req.Name, hash, otp, otpExpires,
	).Scan(&userID)
	if err != nil {
		log.Printf("signup: insert error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Send OTP email (non-blocking — log error but don't fail signup)
	go func() {
		if err := helpers.SendOTPEmail(req.Email, otp, "signup"); err != nil {
			log.Printf("signup: email send error: %v", err)
		}
	}()

	helpers.JSON(w, http.StatusCreated, map[string]interface{}{
		"user_id":    userID,
		"email":      req.Email,
		"name":       req.Name,
		"isVerified": false,
	})
}

// ─── POST /verify-otp ───────────────────────────────────

func VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		OTP   string `json:"otp"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var userID string
	var storedOTP *string
	var otpExpires *time.Time

	err := db.Pool.QueryRow(ctx,
		`SELECT id, otp_code, otp_expires FROM users WHERE email = $1`, req.Email,
	).Scan(&userID, &storedOTP, &otpExpires)
	if err != nil {
		helpers.Error(w, http.StatusNotFound, "user not found")
		return
	}

	if storedOTP == nil || otpExpires == nil {
		helpers.Error(w, http.StatusBadRequest, "no OTP requested")
		return
	}
	if time.Now().After(*otpExpires) {
		helpers.Error(w, http.StatusBadRequest, "OTP has expired")
		return
	}
	if *storedOTP != req.OTP {
		helpers.Error(w, http.StatusBadRequest, "invalid OTP")
		return
	}

	// Mark verified and clear OTP
	_, err = db.Pool.Exec(ctx,
		`UPDATE users SET is_verified = true, otp_code = NULL, otp_expires = NULL WHERE id = $1`,
		userID,
	)
	if err != nil {
		log.Printf("verify-otp: update error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"isVerified": true,
	})
}

// ─── POST /login ─────────────────────────────────────────

func Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var userID, name, passwordHash string
	var isVerified bool

	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, password_hash, is_verified FROM users WHERE email = $1`, req.Email,
	).Scan(&userID, &name, &passwordHash, &isVerified)
	if err != nil {
		helpers.Error(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if !helpers.CheckPassword(passwordHash, req.Password) {
		helpers.Error(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	// If user is not verified, generate a new OTP and email it
	if !isVerified {
		otp := helpers.GenerateOTP()
		otpExpires := time.Now().Add(10 * time.Minute)
		log.Printf("login: OTP for unverified user %s is %s", req.Email, otp)

		_, err = db.Pool.Exec(ctx,
			`UPDATE users SET otp_code = $1, otp_expires = $2 WHERE id = $3`,
			otp, otpExpires, userID,
		)
		if err != nil {
			log.Printf("login: otp update error: %v", err)
		}

		go func() {
			if err := helpers.SendOTPEmail(req.Email, otp, "login"); err != nil {
				log.Printf("login: email send error: %v", err)
			}
		}()
	}

	token, err := helpers.GenerateJWT(userID, req.Email)
	if err != nil {
		log.Printf("login: jwt error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"user_id":       userID,
		"name":          name,
		"email":         req.Email,
		"isOtpVerified": isVerified,
		"token":         token,
	})
}

// ─── GET /auth/validate ──────────────────────────────────

func Validate(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var id, name, email string
	var isVerified bool

	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, email, is_verified FROM users WHERE id = $1`, userID,
	).Scan(&id, &name, &email, &isVerified)
	if err != nil {
		log.Printf("validate: query error: %v", err)
		helpers.Error(w, http.StatusNotFound, "user not found")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"user": map[string]interface{}{
			"id":         id,
			"name":       name,
			"email":      email,
			"isVerified": isVerified,
		},
	})
}

// ─── POST /auth/logout/ ─────────────────────────────────

func Logout(w http.ResponseWriter, r *http.Request) {
	helpers.JSON(w, http.StatusOK, map[string]interface{}{})
}

// ─── POST /auth/users/reset_password/ ───────────────────

func ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var userID string
	err := db.Pool.QueryRow(ctx,
		`SELECT id FROM users WHERE email = $1`, req.Email,
	).Scan(&userID)
	if err != nil {
		// Return 200 even if user not found to avoid email enumeration
		helpers.JSON(w, http.StatusOK, map[string]interface{}{})
		return
	}

	otp := helpers.GenerateOTP()
	otpExpires := time.Now().Add(10 * time.Minute)
	log.Printf("reset_password: OTP for %s is %s", req.Email, otp)

	_, err = db.Pool.Exec(ctx,
		`UPDATE users SET otp_code = $1, otp_expires = $2 WHERE id = $3`,
		otp, otpExpires, userID,
	)
	if err != nil {
		log.Printf("reset_password: update error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Send reset OTP email
	go func() {
		if err := helpers.SendOTPEmail(req.Email, otp, "reset"); err != nil {
			log.Printf("reset_password: email send error: %v", err)
		}
	}()

	helpers.JSON(w, http.StatusOK, map[string]interface{}{})
}

// ─── POST /auth/users/reset_password_confirm/ ───────────

func ResetPasswordConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID           string `json:"uid"`
		Token         string `json:"token"`
		NewPassword   string `json:"new_password"`
		ReNewPassword string `json:"re_new_password"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.NewPassword) < 8 {
		helpers.Error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if req.NewPassword != req.ReNewPassword {
		helpers.Error(w, http.StatusBadRequest, "passwords do not match")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// uid = user UUID, token = OTP code
	var storedOTP *string
	var otpExpires *time.Time

	err := db.Pool.QueryRow(ctx,
		`SELECT otp_code, otp_expires FROM users WHERE id = $1`, req.UID,
	).Scan(&storedOTP, &otpExpires)
	if err != nil {
		helpers.Error(w, http.StatusNotFound, "user not found")
		return
	}

	if storedOTP == nil || otpExpires == nil {
		helpers.Error(w, http.StatusBadRequest, "no password reset requested")
		return
	}
	if time.Now().After(*otpExpires) {
		helpers.Error(w, http.StatusBadRequest, "OTP has expired")
		return
	}
	if *storedOTP != req.Token {
		helpers.Error(w, http.StatusBadRequest, "invalid OTP")
		return
	}

	hash, err := helpers.HashPassword(req.NewPassword)
	if err != nil {
		log.Printf("reset_password_confirm: hash error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	_, err = db.Pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, otp_code = NULL, otp_expires = NULL WHERE id = $2`,
		hash, req.UID,
	)
	if err != nil {
		log.Printf("reset_password_confirm: update error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{})
}
