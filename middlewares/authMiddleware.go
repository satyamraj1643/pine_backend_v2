package middleware

import (
	"log"
	"net/http"
	"strings"
    "context"
	"github.com/satyamraj1643/pine_backend_v2/utilities"
)

func TokenValidateMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		if authHeader == "" {
			http.Error(w, "Authorization header is required", http.StatusForbidden)
			log.Println("Authorization header is required")
			return
		}

		authHeaderSplit := strings.Split(authHeader, " ")

		if len(authHeaderSplit) != 2 || authHeaderSplit[0] != "Bearer" {
			http.Error(w, "Invalid token format", http.StatusForbidden)
			log.Println("Invalid token format")
			return
		}

		token := authHeaderSplit[1]
		log.Println(" + token : ", token)

		// TO-DO add middleware authorisation here.

		claims, ok := utilities.VerifyJWT(token)

		if !ok {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}


		ctx  := context.WithValue(r.Context(), utilities.UserIDKey, claims.UserID)
		ctx =  context.WithValue(ctx, utilities.EmailKey, claims.Email)

		// Pass to next handler
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
