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
	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
	mw "github.com/satyamraj1643/pine_backend_v2/internal/middleware"
	"github.com/satyamraj1643/pine_backend_v2/internal/tracing"
)

func main() {
	// Load .env (overrides system env so .env is the source of truth)
	if err := godotenv.Overload(); err != nil {
		log.Println("No .env file found, using system env")
	}

	// Connect to Postgres + Redis
	db.Connect()
	defer db.Close()
	cache.Connect()
	defer cache.Close()

	helpers.LogSMTPConfig()

	// Initialize LangSmith tracing
	tracing.Init()

	// Build the mux
	mux := http.NewServeMux()

	// ─── Health check ──────────────────────────────────────
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

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
	mux.Handle("PATCH /auth/update-profile", mw.Auth(http.HandlerFunc(handler.UpdateProfile)))
	mux.Handle("POST /auth/logout/", mw.Auth(http.HandlerFunc(handler.Logout)))
	mux.Handle("DELETE /auth/delete-account", mw.Auth(http.HandlerFunc(handler.DeleteAccount)))

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
	mux.Handle("PUT /collections/update/{id}", mw.Auth(http.HandlerFunc(handler.UpdateCollection)))

	// Moods
	mux.Handle("POST /moods/create-new", mw.Auth(http.HandlerFunc(handler.CreateMood)))
	mux.Handle("GET /moods/all", mw.Auth(http.HandlerFunc(handler.GetAllMoods)))
	mux.Handle("DELETE /moods/delete/{id}", mw.Auth(http.HandlerFunc(handler.DeleteMood)))
	mux.Handle("PUT /moods/update/{id}", mw.Auth(http.HandlerFunc(handler.UpdateMood)))

	// AI features
	mux.Handle("POST /ai/reflect", mw.Auth(http.HandlerFunc(handler.AIReflect)))
	mux.Handle("POST /ai/chat", mw.Auth(http.HandlerFunc(handler.AIChat)))
	mux.Handle("POST /ai/suggest-mood", mw.Auth(http.HandlerFunc(handler.AISuggestMood)))
	mux.Handle("POST /ai/ask", mw.Auth(http.HandlerFunc(handler.AIAsk)))
	mux.Handle("GET /ai/weekly-recap", mw.Auth(http.HandlerFunc(handler.AIWeeklyRecap)))
	mux.Handle("GET /ai/insights", mw.Auth(http.HandlerFunc(handler.AIInsights)))
	mux.Handle("GET /ai/personality", mw.Auth(http.HandlerFunc(handler.AIPersonality)))
	mux.Handle("GET /ai/health", mw.Auth(http.HandlerFunc(handler.AIHealth)))

	// Exports
	mux.Handle("POST /exports/log", mw.Auth(http.HandlerFunc(handler.LogExport)))
	mux.Handle("GET /exports/latest", mw.Auth(http.HandlerFunc(handler.GetLatestExport)))

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
