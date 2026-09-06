package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
)

func TestInactiveAccountsCannotUseValidTokens(t *testing.T) {
	t.Setenv("JWT_SECRET", "a-test-only-secret")
	token, err := helpers.GenerateJWT("user-id", "a@example.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		active bool
		err    error
		status int
	}{
		{"verified", true, nil, 200},
		{"unverified or deleted", false, nil, 401},
		{"database unavailable", false, errors.New("offline"), 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			handler := authWithAccountCheck(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if helpers.GetUserID(r) != "user-id" {
					t.Fatal("wrong user context")
				}
				w.WriteHeader(200)
			}), func(_ context.Context, id string) (bool, error) {
				if id != "user-id" {
					t.Fatal("wrong account")
				}
				return test.active, test.err
			})
			r := httptest.NewRequest("GET", "/entry/all", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.status || called != (test.status == 200) {
				t.Fatalf("status %d, handler reached %v", w.Code, called)
			}
		})
	}
}

func TestCORSRejectsWildcardAndAddsPrivacyHeaders(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "*")
	if allowedOrigin("https://untrusted.example") != "" {
		t.Fatal("wildcard reflected with credentials")
	}
	w := httptest.NewRecorder()
	CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Header().Get("Vary") != "Origin" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing cache protection")
	}
}
