package coreactions

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/satyamraj1643/pine_backend_v2/entities"
	db "github.com/satyamraj1643/pine_backend_v2/sql"
	"github.com/satyamraj1643/pine_backend_v2/utilities"
)

func HandleSignup(w http.ResponseWriter, r *http.Request) {
    fmt.Println("in signup")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Body == nil {
		http.Error(w, "Body is empty", http.StatusBadRequest)
		return
	}

	defer r.Body.Close()

	var payload entities.SignupRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Malformed request body", http.StatusBadRequest)
		return
	}

	//  Normalize & validate input
	payload.Email = strings.ToLower(strings.TrimSpace(payload.Email))
	payload.FirstName = strings.TrimSpace(payload.FirstName)
	payload.LastName = strings.TrimSpace(payload.LastName)

	if payload.Email == "" || payload.Password == "" {
		http.Error(w, "Email and password are required", http.StatusBadRequest)
		return
	}

	dbConn := db.GetDB()
	ctx := context.Background()

	//  Check if email already exists
	var existingEmail string
	err := dbConn.QueryRow(ctx, "SELECT email FROM users WHERE email=$1", payload.Email).Scan(&existingEmail)

	if err == nil {
		http.Error(w, "User already exists", http.StatusConflict)
		return
	} else if err != pgx.ErrNoRows {
		http.Error(w, "Database error checking user", http.StatusInternalServerError)
		return
	}
	//  Hash password
	hashingInstance := utilities.NewBcryptPasswordService()
	hashedPassword, err := hashingInstance.HashPassword(payload.Password)

	if err != nil {
		log.Println("Password hashing failed:", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	//  Start transaction
	tx, err := dbConn.Begin(ctx)
	if err != nil {
		http.Error(w, "Database transaction error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	//  Insert into users table
	var userID string
	err = tx.QueryRow(
		ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ($1, $2)
		 RETURNING id`,
		payload.Email,
		hashedPassword,
	).Scan(&userID)

	if err != nil {
		log.Println("User insert failed:", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	//  Insert into user_profiles table
	_, err = tx.Exec(
		ctx,
		`INSERT INTO user_profiles (user_id, first_name, last_name)
		 VALUES ($1, $2, $3)`,
		userID,
		payload.FirstName,
		payload.LastName,
	)

	if err != nil {
		log.Println("Profile insert failed:", err)
		http.Error(w, "Failed to create user profile", http.StatusInternalServerError)
		return
	}

	//  Commit transaction
	if err = tx.Commit(ctx); err != nil {
		log.Println("Transaction commit failed:", err)
		http.Error(w, "Database commit error", http.StatusInternalServerError)
		return
	}

	// Generate JWT token
	token, err := utilities.GenerateJWT(userID, payload.Email)
	if err != nil {
		log.Println("JWT generation failed:", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}


	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Signup successful",
		"user": map[string]interface{}{
			"id":         userID,
			"email":      payload.Email,
			"first_name": payload.FirstName,
			"last_name":  payload.LastName,
		},
		"access_token": token,
	})
}
