package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
)

// allowedOrigins returns the request origin if it's in our whitelist.
func allowedOrigin(origin string) string {
	allowed := []string{
		"http://localhost:5173",
		"https://pine-ut4n.onrender.com",
	}
	for _, o := range allowed {
		if origin == o {
			return o
		}
	}
	return ""
}

// CORS handles preflight and sets headers for all requests.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := allowedOrigin(origin)
		if allowed != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowed)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Auth validates the Bearer token and injects user_id + email into context.
func Auth(next http.Handler) http.Handler {
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

		ctx := context.WithValue(r.Context(), helpers.CtxUserID, claims.UserID)
		ctx = context.WithValue(ctx, helpers.CtxEmail, claims.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
