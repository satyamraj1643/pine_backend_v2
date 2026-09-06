package helpers

import (
	"regexp"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestOTPFormat(t *testing.T) {
	for i := 0; i < 50; i++ {
		otp, err := GenerateOTP()
		if err != nil || !regexp.MustCompile(`^[0-9]{6}$`).MatchString(otp) {
			t.Fatalf("invalid OTP format or generation error")
		}
	}
}

func TestJWTRequiresSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	if _, err := GenerateJWT("user", "a@example.test"); err == nil {
		t.Fatal("signed with missing secret")
	}
	if _, err := VerifyJWT("anything"); err == nil {
		t.Fatal("verified with missing secret")
	}
}

func TestJWTLegacyCompatibleAndStrict(t *testing.T) {
	t.Setenv("JWT_SECRET", "a-test-only-secret-with-enough-entropy")
	valid, err := GenerateJWT("user", "a@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyJWT(valid); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		method jwt.SigningMethod
		claims JWTClaims
	}{
		{"wrong algorithm", jwt.SigningMethodHS384, JWTClaims{UserID: "user", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}},
		{"no expiration", jwt.SigningMethodHS256, JWTClaims{UserID: "user"}},
		{"expired", jwt.SigningMethodHS256, JWTClaims{UserID: "user", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}}},
		{"no user", jwt.SigningMethodHS256, JWTClaims{RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			token, err := jwt.NewWithClaims(test.method, test.claims).SignedString(jwtSecret())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyJWT(token); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
}
