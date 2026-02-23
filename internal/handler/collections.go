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

// ─── Create Collection ──────────────────────────────────

func CreateCollection(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)

	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := helpers.Decode(r, &body); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" || body.Color == "" {
		helpers.Error(w, http.StatusBadRequest, "name and color are required")
		return
	}

	var id int
	var name, color string
	err := db.Pool.QueryRow(
		context.Background(),
		`INSERT INTO collections (user_id, name, color)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, color`,
		userID, body.Name, body.Color,
	).Scan(&id, &name, &color)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to create collection")
		return
	}

	// Invalidate collections cache
	_ = cache.Del(context.Background(), "collections:"+userID)

	helpers.JSON(w, http.StatusCreated, map[string]interface{}{
		"ID":    id,
		"Name":  name,
		"Color": color,
	})
}

// ─── Get All Collections ────────────────────────────────

type collectionRow struct {
	ID       int    `json:"ID"`
	Name     string `json:"Name"`
	Color    string `json:"Color"`
	Entries  int    `json:"Entries"`
	Chapters int    `json:"Chapters"`
}

func GetAllCollections(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	ctx := context.Background()
	cacheKey := "collections:" + userID

	// Try cache first
	if cached, err := cache.Get(ctx, cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(cached))
		return
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT
			c.id,
			c.name,
			c.color,
			COALESCE(ec.entry_count, 0)   AS entries,
			COALESCE(cc.chapter_count, 0)  AS chapters
		 FROM collections c
		 LEFT JOIN (
			SELECT collection_id, COUNT(*) AS entry_count
			FROM entry_collections
			GROUP BY collection_id
		 ) ec ON ec.collection_id = c.id
		 LEFT JOIN (
			SELECT collection_id, COUNT(*) AS chapter_count
			FROM chapter_collections
			GROUP BY collection_id
		 ) cc ON cc.collection_id = c.id
		 WHERE c.user_id = $1
		 ORDER BY c.created_at DESC`,
		userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to fetch collections")
		return
	}
	defer rows.Close()

	collections := []collectionRow{}
	for rows.Next() {
		var cr collectionRow
		if err := rows.Scan(&cr.ID, &cr.Name, &cr.Color, &cr.Entries, &cr.Chapters); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to scan collection")
			return
		}
		collections = append(collections, cr)
	}

	resp := map[string]interface{}{
		"collections": collections,
	}

	// Store in cache
	if data, err := json.Marshal(resp); err == nil {
		_ = cache.Set(ctx, cacheKey, data, 5*time.Minute)
	}

	helpers.JSON(w, http.StatusOK, resp)
}

// ─── Delete Collection ──────────────────────────────────

func DeleteCollection(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)

	id, err := helpers.PathParamInt(r.URL.Path, "/collections/delete/")
	if err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid collection id")
		return
	}

	ctx := context.Background()

	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM collections WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to delete collection")
		return
	}
	if tag.RowsAffected() == 0 {
		helpers.Error(w, http.StatusNotFound, "collection not found")
		return
	}

	// Invalidate collections, entries, and chapters caches
	_ = cache.Del(ctx, "collections:"+userID)
	_ = cache.Del(ctx, "entries:"+userID)
	_ = cache.Del(ctx, "chapters:"+userID)

	helpers.JSON(w, http.StatusOK, map[string]bool{"success": true})
}
