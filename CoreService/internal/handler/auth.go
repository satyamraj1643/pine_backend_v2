package handler

import (
	"context"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/satyamraj1643/pine_backend_v2/internal/cache"
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
	if !allowAuthAttempt(w, r, "signup", req.Email, 5) {
		return
	}

	if len(req.Name) < 2 || len(req.Name) > 200 {
		helpers.Error(w, http.StatusBadRequest, "Name must be between 2 and 200 bytes")
		return
	}

	// Validate
	if !emailRe.MatchString(req.Email) {
		helpers.Error(w, http.StatusBadRequest, "invalid email format")
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 72 {
		helpers.Error(w, http.StatusBadRequest, "password must be at least 8 characters and at most 72 bytes")
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
	otp, err := helpers.GenerateOTP()
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "Unable to generate verification code")
		return
	}
	otpExpires := time.Now().Add(10 * time.Minute)

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
	if !allowAuthAttempt(w, r, "verify", req.Email, 5) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var userID string
	var storedOTP *string
	var otpExpires *time.Time

	err := db.Pool.QueryRow(ctx,
		`SELECT id, otp_code, otp_expires FROM users WHERE email = $1 AND is_verified = false`, req.Email,
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
	var name string
	err = db.Pool.QueryRow(ctx,
		`UPDATE users SET is_verified = true, otp_code = NULL, otp_expires = NULL
		 WHERE id = $1 AND is_verified = false AND otp_code = $2 AND otp_expires > NOW() RETURNING name`,
		userID, req.OTP,
	).Scan(&name)
	if err != nil {
		log.Printf("verify-otp: update error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Generate JWT so the client is immediately authenticated (same as /login)
	token, err := helpers.GenerateJWT(userID, req.Email)
	if err != nil {
		log.Printf("verify-otp: jwt error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"user_id":       userID,
		"name":          name,
		"email":         req.Email,
		"isOtpVerified": true,
		"token":         token,
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
	if !allowAuthAttempt(w, r, "login", req.Email, 10) {
		return
	}

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
		otp, err := helpers.GenerateOTP()
		if err != nil {
			helpers.Error(w, http.StatusInternalServerError, "Unable to generate verification code")
			return
		}
		otpExpires := time.Now().Add(10 * time.Minute)

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
	var profilePicture *string

	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, email, is_verified, profile_picture FROM users WHERE id = $1`, userID,
	).Scan(&id, &name, &email, &isVerified, &profilePicture)
	if err != nil {
		log.Printf("validate: query error: %v", err)
		helpers.Error(w, http.StatusNotFound, "user not found")
		return
	}

	pp := ""
	if profilePicture != nil {
		pp = *profilePicture
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"user": map[string]interface{}{
			"id":              id,
			"name":            name,
			"email":           email,
			"isVerified":      isVerified,
			"profile_picture": pp,
		},
	})
}

// ─── PATCH /auth/update-profile ──────────────────────────

func UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Name           string  `json:"name"`
		ProfilePicture *string `json:"profile_picture"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		helpers.Error(w, http.StatusBadRequest, "name cannot be empty")
		return
	}
	if len(req.Name) > 200 {
		helpers.Error(w, http.StatusBadRequest, "name must be 200 characters or fewer")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if req.ProfilePicture != nil {
		// Limit profile picture to ~500KB base64
		if len(*req.ProfilePicture) > 700000 {
			helpers.Error(w, http.StatusBadRequest, "profile picture too large (max 500KB)")
			return
		}
		_, err := db.Pool.Exec(ctx,
			`UPDATE users SET name = $1, profile_picture = $2 WHERE id = $3`, req.Name, *req.ProfilePicture, userID,
		)
		if err != nil {
			log.Printf("update-profile: update error: %v", err)
			helpers.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		helpers.JSON(w, http.StatusOK, map[string]interface{}{
			"updated":         true,
			"name":            req.Name,
			"profile_picture": *req.ProfilePicture,
		})
		return
	}

	_, err := db.Pool.Exec(ctx,
		`UPDATE users SET name = $1 WHERE id = $2`, req.Name, userID,
	)
	if err != nil {
		log.Printf("update-profile: update error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"updated": true,
		"name":    req.Name,
	})
}

// ─── POST /auth/logout/ ─────────────────────────────────

func Logout(w http.ResponseWriter, r *http.Request) {
	helpers.JSON(w, http.StatusOK, map[string]interface{}{})
}

// ─── DELETE /auth/delete-account ─────────────────────────

func DeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// CASCADE on users table handles entries, chapters, collections, moods
	_, err := db.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		log.Printf("delete-account: error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "failed to delete account")
		return
	}

	_ = cache.Del(ctx, "entries:"+userID, "chapters:"+userID, "collections:"+userID, "moods:"+userID)
	log.Printf("delete-account: user %s deleted", userID)
	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"deleted": true,
	})
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
	if !allowAuthAttempt(w, r, "reset", req.Email, 5) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var userID string
	err := db.Pool.QueryRow(ctx,
		`SELECT id FROM users WHERE email = $1 AND is_verified = true`, req.Email,
	).Scan(&userID)
	if err != nil {
		// Return 200 even if user not found to avoid email enumeration
		helpers.JSON(w, http.StatusOK, map[string]interface{}{})
		return
	}

	otp, err := helpers.GenerateOTP()
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "Unable to generate verification code")
		return
	}
	otpExpires := time.Now().Add(10 * time.Minute)

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
		Email         string `json:"email"`
		Token         string `json:"token"`
		NewPassword   string `json:"new_password"`
		ReNewPassword string `json:"re_new_password"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.NewPassword) < 8 || len(req.NewPassword) > 72 {
		helpers.Error(w, http.StatusBadRequest, "password must be at least 8 characters and at most 72 bytes")
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
		`SELECT id, otp_code, otp_expires FROM users WHERE (id::text = $1 OR ($1 = '' AND email = $2)) AND is_verified = true`, req.UID, strings.ToLower(strings.TrimSpace(req.Email)),
	).Scan(&req.UID, &storedOTP, &otpExpires)
	if err != nil {
		helpers.Error(w, http.StatusNotFound, "user not found")
		return
	}

	if !allowAuthAttempt(w, r, "reset-confirm", req.UID, 5) {
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

	result, err := db.Pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, otp_code = NULL, otp_expires = NULL
		 WHERE id = $2 AND is_verified = true AND otp_code = $3 AND otp_expires > NOW()`,
		hash, req.UID, req.Token,
	)
	if err != nil {
		log.Printf("reset_password_confirm: update error: %v", err)
		helpers.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if result.RowsAffected() != 1 {
		helpers.Error(w, http.StatusBadRequest, "Invalid or expired reset code")
		return
	}
	helpers.JSON(w, http.StatusOK, map[string]interface{}{})
}
