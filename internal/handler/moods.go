package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/satyamraj1643/pine_backend_v2/internal/cache"
	"github.com/satyamraj1643/pine_backend_v2/internal/db"
	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
)

// ─── Create Mood ────────────────────────────────────────

func CreateMood(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)

	var body struct {
		Name  string `json:"name"`
		Emoji string `json:"emoji"`
		Color string `json:"color"`
	}
	if err := helpers.Decode(r, &body); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" || body.Emoji == "" || body.Color == "" {
		helpers.Error(w, http.StatusBadRequest, "name, emoji, and color are required")
		return
	}

	_, err := db.Pool.Exec(
		context.Background(),
		`INSERT INTO moods (user_id, name, emoji, color)
		 VALUES ($1, $2, $3, $4)`,
		userID, body.Name, body.Emoji, body.Color,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to create mood")
		return
	}

	// Invalidate moods cache
	_ = cache.Del(context.Background(), "moods:"+userID)

	helpers.JSON(w, http.StatusCreated, map[string]bool{"success": true})
}

// ─── Get All Moods ──────────────────────────────────────

type moodRow struct {
	ID        int    `json:"ID"`
	Name      string `json:"Name"`
	Emoji     string `json:"Emoji"`
	Color     string `json:"Color"`
	CreatedAt string `json:"CreatedAt"`
}

func GetAllMoods(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	ctx := context.Background()
	cacheKey := "moods:" + userID

	// Try cache first
	if cached, err := cache.Get(ctx, cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(cached))
		return
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT id, name, emoji, color, created_at
		 FROM moods
		 WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to fetch moods")
		return
	}
	defer rows.Close()

	moods := []moodRow{}
	for rows.Next() {
		var mr moodRow
		var createdAt time.Time
		if err := rows.Scan(&mr.ID, &mr.Name, &mr.Emoji, &mr.Color, &createdAt); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to scan mood")
			return
		}
		mr.CreatedAt = createdAt.Format(time.RFC3339)
		moods = append(moods, mr)
	}

	resp := map[string]interface{}{
		"moods": moods,
	}

	// Store in cache
	if data, err := json.Marshal(resp); err == nil {
		_ = cache.Set(ctx, cacheKey, data, 5*time.Minute)
	}

	helpers.JSON(w, http.StatusOK, resp)
}

// ─── Delete Mood ────────────────────────────────────────

func DeleteMood(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)

	id, err := helpers.PathParamInt(r.URL.Path, "/moods/delete/")
	if err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid mood id")
		return
	}

	ctx := context.Background()

	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM moods WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to delete mood")
		return
	}
	if tag.RowsAffected() == 0 {
		helpers.Error(w, http.StatusNotFound, "mood not found")
		return
	}

	// Invalidate moods and entries caches
	_ = cache.Del(ctx, "moods:"+userID)
	_ = cache.Del(ctx, "entries:"+userID)

	helpers.JSON(w, http.StatusOK, map[string]bool{"success": true})
}
