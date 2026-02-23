package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/satyamraj1643/pine_backend_v2/internal/cache"
	"github.com/satyamraj1643/pine_backend_v2/internal/db"
	"github.com/satyamraj1643/pine_backend_v2/internal/handler"
	mw "github.com/satyamraj1643/pine_backend_v2/internal/middleware"
)

func main() {
	// Load .env
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system env")
	}

	// Connect to Postgres + Redis
	db.Connect()
	defer db.Close()
	cache.Connect()
	defer cache.Close()

	// Build the mux
	mux := http.NewServeMux()

	// ─── Public auth routes (no token needed) ───────────────
	mux.HandleFunc("POST /signup", handler.Signup)
	mux.HandleFunc("POST /login", handler.Login)
	mux.HandleFunc("POST /verify-otp", handler.VerifyOTP)
	mux.HandleFunc("POST /auth/users/reset_password/", handler.ResetPassword)
	mux.HandleFunc("POST /auth/users/reset_password_confirm/", handler.ResetPasswordConfirm)

	// ─── Protected routes (require Bearer token) ────────────
	// We create a sub-mux for protected routes, wrap it with Auth middleware,
	// then mount each route individually on the main mux.

	// Auth
	mux.Handle("GET /auth/validate", mw.Auth(http.HandlerFunc(handler.Validate)))
	mux.Handle("POST /auth/logout/", mw.Auth(http.HandlerFunc(handler.Logout)))

	// Entries
	mux.Handle("POST /entries/create-new", mw.Auth(http.HandlerFunc(handler.CreateEntry)))
	mux.Handle("GET /entries/all", mw.Auth(http.HandlerFunc(handler.GetAllEntries)))
	mux.Handle("DELETE /entries/delete/{id}", mw.Auth(http.HandlerFunc(handler.DeleteEntry)))
	mux.Handle("POST /entries/archive/{id}", mw.Auth(http.HandlerFunc(handler.ArchiveEntry)))
	mux.Handle("POST /entries/mark-favourite/{id}", mw.Auth(http.HandlerFunc(handler.MarkFavouriteEntry)))
	// PATCH /entries/details/{id} — use a catch-all since Go 1.22+ patterns
	mux.Handle("PATCH /entries/details/{id}", mw.Auth(http.HandlerFunc(handler.UpdateEntry)))

	// Chapters
	mux.Handle("POST /chapters/create-new", mw.Auth(http.HandlerFunc(handler.CreateChapter)))
	mux.Handle("GET /chapters/all", mw.Auth(http.HandlerFunc(handler.GetAllChapters)))
	mux.Handle("PUT /chapters/update/{id}", mw.Auth(http.HandlerFunc(handler.UpdateChapter)))
	mux.Handle("DELETE /chapters/delete/{id}", mw.Auth(http.HandlerFunc(handler.DeleteChapter)))
	mux.Handle("POST /chapters/archive/{id}", mw.Auth(http.HandlerFunc(handler.ArchiveChapter)))
	mux.Handle("POST /chapters/mark-favourite/{id}", mw.Auth(http.HandlerFunc(handler.MarkFavouriteChapter)))

	// Collections
	mux.Handle("POST /collections/create-new", mw.Auth(http.HandlerFunc(handler.CreateCollection)))
	mux.Handle("GET /collections/all", mw.Auth(http.HandlerFunc(handler.GetAllCollections)))
	mux.Handle("DELETE /collections/delete/{id}", mw.Auth(http.HandlerFunc(handler.DeleteCollection)))

	// Moods
	mux.Handle("POST /moods/create-new", mw.Auth(http.HandlerFunc(handler.CreateMood)))
	mux.Handle("GET /moods/all", mw.Auth(http.HandlerFunc(handler.GetAllMoods)))
	mux.Handle("DELETE /moods/delete/{id}", mw.Auth(http.HandlerFunc(handler.DeleteMood)))

	// Wrap everything with CORS
	finalHandler := mw.CORS(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Strip trailing slashes
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") && !strings.Contains(r.URL.Path, "/auth/") {
			r.URL.Path = strings.TrimRight(r.URL.Path, "/")
		}
		finalHandler.ServeHTTP(w, r)
	})

	log.Printf("Pine backend running on :%s\n", port)
	if err := http.ListenAndServe(":"+port, wrapped); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
