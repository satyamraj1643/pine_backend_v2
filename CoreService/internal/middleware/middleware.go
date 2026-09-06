package middleware

import (
	"context"
	"github.com/satyamraj1643/pine_backend_v2/internal/db"
	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
	"net/http"
	"os"
	"strings"
	"time"
)

// allowedOrigin returns the request origin if it's in our whitelist.
// Origins are read from the ALLOWED_ORIGINS env var (comma-separated).
// Use explicit origins; credentialed requests must not reflect arbitrary origins.

func allowedOrigin(origin string) string {
	if origin == "" {
		return ""
	}
	raw := os.Getenv("ALLOWED_ORIGINS")
	if raw == "" {
		raw = "http://localhost:5173,http://localhost:3000,https://pine.brink.co.in"
	}
	cleanOrigin := strings.TrimRight(origin, "/")
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o != "" && o != "*" && strings.EqualFold(cleanOrigin, o) {
			return origin
		}
	}
	return ""
}

// CORS handles preflight and sets headers for all requests.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
		origin := r.Header.Get("Origin")
		allowed := allowedOrigin(origin)
		if allowed != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowed)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Accept, Origin")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Auth validates the Bearer token and injects user_id + email into context.
func Auth(next http.Handler) http.Handler {
	return authWithAccountCheck(next, func(ctx context.Context, userID string) (bool, error) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		var active bool
		err := db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND is_verified = true)`, userID).Scan(&active)
		return active, err
	})
}

func authWithAccountCheck(next http.Handler, check func(context.Context, string) (bool, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			helpers.Error(w, http.StatusUnauthorized, "Missing or malformed authorization header")
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := helpers.VerifyJWT(tokenStr)
		if err != nil {
			helpers.Error(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}
		active, err := check(r.Context(), claims.UserID)
		if err != nil {
			helpers.Error(w, http.StatusServiceUnavailable, "Unable to verify account. Please try again.")
			return
		}
		if !active {
			helpers.Error(w, http.StatusUnauthorized, "Account is not active or email is not verified")
			return
		}

		ctx := context.WithValue(r.Context(), helpers.CtxUserID, claims.UserID)
		ctx = context.WithValue(ctx, helpers.CtxEmail, claims.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
