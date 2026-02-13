package handlers

import (
	"fmt"
	"net/http"
	"strings"

	authHandlers "github.com/satyamraj1643/pine_backend_v2/handlers/authHandlers"
	middleware "github.com/satyamraj1643/pine_backend_v2/middlewares"
)

// HandleRequest handles all incoming requests
func HandleRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	fmt.Println("+ Path:", path)

	// Public routes (no auth required)
	if path == "/auth/core/signup" || path == "/auth/core/login" {
		authHandlers.AuthHandler(w, r)
		return
	}

	// Protected auth routes
	if strings.HasPrefix(path, "/auth") {
		handler := middleware.TokenValidateMiddleware(http.HandlerFunc(authHandlers.AuthHandler))
		handler.ServeHTTP(w, r)
		return
	}

	// Other routes (future)
	http.NotFound(w, r)
}

