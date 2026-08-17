package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAllowedOrigin(t *testing.T) {
	os.Setenv("ALLOWED_ORIGINS", "http://localhost:5173, https://pine.brink.co.in, https://app.pine.com/")
	defer os.Unsetenv("ALLOWED_ORIGINS")

	tests := []struct {
		origin   string
		expected string
	}{
		{"https://pine.brink.co.in", "https://pine.brink.co.in"},
		{"https://pine.brink.co.in/", "https://pine.brink.co.in/"},
		{"http://localhost:5173", "http://localhost:5173"},
		{"https://app.pine.com", "https://app.pine.com"},
		{"https://malicious.com", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := allowedOrigin(tt.origin)
		if got != tt.expected {
			t.Errorf("allowedOrigin(%q) = %q; want %q", tt.origin, got, tt.expected)
		}
	}
}

func TestCORSPreflight(t *testing.T) {
	os.Setenv("ALLOWED_ORIGINS", "https://pine.brink.co.in")
	defer os.Unsetenv("ALLOWED_ORIGINS")

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := CORS(dummyHandler)

	req := httptest.NewRequest(http.MethodOptions, "/signup", nil)
	req.Header.Set("Origin", "https://pine.brink.co.in")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204 No Content, got %d", rec.Code)
	}

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://pine.brink.co.in" {
		t.Errorf("expected Access-Control-Allow-Origin to be 'https://pine.brink.co.in', got %q", got)
	}

	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Errorf("expected Access-Control-Allow-Methods header to be set")
	}
}
