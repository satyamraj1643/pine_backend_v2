package helpers

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// ─── JSON helpers ────────────────────────────────────────

func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"detail": msg})
}

func Decode(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// ─── Password ────────────────────────────────────────────

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func CheckPassword(hashed, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)) == nil
}

// ─── JWT ─────────────────────────────────────────────────

type JWTClaims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

func jwtSecret() []byte {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		return nil
	}
	return []byte(s)
}

func GenerateJWT(userID, email string) (string, error) {
	if len(jwtSecret()) == 0 {
		return "", fmt.Errorf("JWT_SECRET must be configured")
	}
	claims := JWTClaims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret())
}

func VerifyJWT(tokenStr string) (*JWTClaims, error) {
	if len(jwtSecret()) == 0 {
		return nil, fmt.Errorf("JWT_SECRET must be configured")
	}
	token, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return jwtSecret(), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid || claims.UserID == "" {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

// ─── OTP ─────────────────────────────────────────────────

func GenerateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// ─── URL helpers ─────────────────────────────────────────

func PathParam(path, prefix string) string {
	s := strings.TrimPrefix(path, prefix)
	s = strings.Trim(s, "/")
	return s
}

func PathParamInt(path, prefix string) (int, error) {
	s := PathParam(path, prefix)
	return strconv.Atoi(s)
}

// ─── Context keys ────────────────────────────────────────

type ctxKey string

const (
	CtxUserID ctxKey = "user_id"
	CtxEmail  ctxKey = "email"
)

func GetUserID(r *http.Request) string {
	v, _ := r.Context().Value(CtxUserID).(string)
	return v
}
