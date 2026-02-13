package utilities

import (
	"os"
	"time"
    "errors"
	"github.com/golang-jwt/jwt/v5"
)

type JWTClaims struct {
	UserID string    `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

func getJWTSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		panic("JWT_SECRET not set in environment variables")
	}
	return []byte(secret)
}

func GenerateJWT(userID string, email string) (string, error) {
	claims := JWTClaims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJWTSecret())
}


func VerifyJWT(tokenString string) (*JWTClaims , bool) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&JWTClaims{},
		func (token *jwt.Token) (interface{}, error){
			// Ensure HMAC Signing
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}

			return getJWTSecret(), nil
		},
	)


	if err != nil || !token.Valid {
		return nil, false
	}

	claims, ok := token.Claims.(*JWTClaims)

	if !ok {
		return nil, false
	}

	return claims, true
}
