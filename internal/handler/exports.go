package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/satyamraj1643/pine_backend_v2/internal/db"
	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
)

// ─── POST /exports/log ──────────────────────────────────

func LogExport(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(helpers.CtxUserID).(string)
	if !ok || userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Format    string `json:"format"`
		SizeBytes int64  `json:"size_bytes"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Format == "" {
		helpers.Error(w, http.StatusBadRequest, "format is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := db.Pool.Exec(ctx,
		`INSERT INTO export_logs (user_id, format, size_bytes) VALUES ($1, $2, $3)`,
		userID, req.Format, req.SizeBytes,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to log export")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{"logged": true})
}

// ─── GET /exports/latest ────────────────────────────────

func GetLatestExport(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(helpers.CtxUserID).(string)
	if !ok || userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var format string
	var sizeBytes int64
	var exportedAt time.Time

	err := db.Pool.QueryRow(ctx,
		`SELECT format, size_bytes, exported_at FROM export_logs
		 WHERE user_id = $1 ORDER BY exported_at DESC LIMIT 1`,
		userID,
	).Scan(&format, &sizeBytes, &exportedAt)

	if err != nil {
		// No exports yet — return empty
		helpers.JSON(w, http.StatusOK, map[string]interface{}{
			"has_export": false,
		})
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"has_export":  true,
		"format":      format,
		"size_bytes":  sizeBytes,
		"exported_at": exportedAt,
	})
}
