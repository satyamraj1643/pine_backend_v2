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

// ─── POST /chapters/create-new ──────────────────────────

func CreateChapter(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Color       string `json:"color"`
		Collection  []int  `json:"collection"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var chapterID int
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO chapters (user_id, title, description, color, is_favourite, is_archived, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, false, false, NOW(), NOW())
		 RETURNING id`,
		userID, req.Title, req.Description, req.Color,
	).Scan(&chapterID)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to create chapter")
		return
	}

	// Insert collection associations
	if len(req.Collection) > 0 {
		for _, colID := range req.Collection {
			_, err := db.Pool.Exec(ctx,
				`INSERT INTO chapter_collections (chapter_id, collection_id) VALUES ($1, $2)`,
				chapterID, colID,
			)
			if err != nil {
				helpers.Error(w, http.StatusInternalServerError, "failed to link collection")
				return
			}
		}
	}

	// Invalidate cache
	cache.DelByPrefix(ctx, "chapters:"+userID)
	cache.DelByPrefix(ctx, "entries:"+userID)

	helpers.JSON(w, http.StatusCreated, map[string]interface{}{
		"created": true,
	})
}

// ─── GET /chapters/all ──────────────────────────────────

func GetAllChapters(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Try cache first
	cacheKey := "chapters:" + userID
	cached, err := cache.Get(ctx, cacheKey)
	if err == nil && cached != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(cached))
		return
	}

	// Fetch chapters
	rows, err := db.Pool.Query(ctx,
		`SELECT id, title, description, color, is_favourite, is_archived, created_at, updated_at
		 FROM chapters
		 WHERE user_id = $1
		 ORDER BY updated_at DESC`,
		userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to fetch chapters")
		return
	}
	defer rows.Close()

	type CollectionItem struct {
		ID    int    `json:"ID"`
		Name  string `json:"Name"`
		Color string `json:"Color"`
	}

	type EntryItem struct {
		ID          int              `json:"ID"`
		Title       string           `json:"Title"`
		Content     string           `json:"Content"`
		Collections []CollectionItem `json:"Collections"`
		IsFavourite bool             `json:"IsFavourite"`
		IsArchived  bool             `json:"IsArchived"`
		UpdatedAt   time.Time        `json:"UpdatedAt"`
	}

	type ChapterItem struct {
		ID          int              `json:"ID"`
		Title       string           `json:"Title"`
		Description string           `json:"Description"`
		Color       string           `json:"Color"`
		Collections []CollectionItem `json:"Collections"`
		Entries     []EntryItem      `json:"Entries"`
		IsFavourite bool             `json:"IsFavourite"`
		IsArchived  bool             `json:"IsArchived"`
		CreatedAt   time.Time        `json:"CreatedAt"`
		UpdatedAt   time.Time        `json:"UpdatedAt"`
	}

	var chapters []ChapterItem

	for rows.Next() {
		var ch ChapterItem
		if err := rows.Scan(&ch.ID, &ch.Title, &ch.Description, &ch.Color, &ch.IsFavourite, &ch.IsArchived, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to scan chapter")
			return
		}
		chapters = append(chapters, ch)
	}
	if err := rows.Err(); err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to iterate chapters")
		return
	}

	// For each chapter, fetch collections and entries
	for i := range chapters {
		// Fetch chapter collections
		colRows, err := db.Pool.Query(ctx,
			`SELECT c.id, c.name, c.color
			 FROM collections c
			 INNER JOIN chapter_collections cc ON cc.collection_id = c.id
			 WHERE cc.chapter_id = $1`,
			chapters[i].ID,
		)
		if err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to fetch chapter collections")
			return
		}

		var cols []CollectionItem
		for colRows.Next() {
			var col CollectionItem
			if err := colRows.Scan(&col.ID, &col.Name, &col.Color); err != nil {
				colRows.Close()
				helpers.Error(w, http.StatusInternalServerError, "failed to scan collection")
				return
			}
			cols = append(cols, col)
		}
		colRows.Close()
		if err := colRows.Err(); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to iterate collections")
			return
		}
		if cols == nil {
			cols = []CollectionItem{}
		}
		chapters[i].Collections = cols

		// Fetch entries for this chapter
		entryRows, err := db.Pool.Query(ctx,
			`SELECT id, title, content, is_favourite, is_archived, updated_at
			 FROM entries
			 WHERE chapter_id = $1 AND user_id = $2
			 ORDER BY updated_at DESC`,
			chapters[i].ID, userID,
		)
		if err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to fetch entries")
			return
		}

		var entries []EntryItem
		for entryRows.Next() {
			var e EntryItem
			if err := entryRows.Scan(&e.ID, &e.Title, &e.Content, &e.IsFavourite, &e.IsArchived, &e.UpdatedAt); err != nil {
				entryRows.Close()
				helpers.Error(w, http.StatusInternalServerError, "failed to scan entry")
				return
			}
			entries = append(entries, e)
		}
		entryRows.Close()
		if err := entryRows.Err(); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to iterate entries")
			return
		}

		// For each entry, fetch its collections
		for j := range entries {
			ecRows, err := db.Pool.Query(ctx,
				`SELECT c.id, c.name, c.color
				 FROM collections c
				 INNER JOIN entry_collections ec ON ec.collection_id = c.id
				 WHERE ec.entry_id = $1`,
				entries[j].ID,
			)
			if err != nil {
				helpers.Error(w, http.StatusInternalServerError, "failed to fetch entry collections")
				return
			}

			var eCols []CollectionItem
			for ecRows.Next() {
				var col CollectionItem
				if err := ecRows.Scan(&col.ID, &col.Name, &col.Color); err != nil {
					ecRows.Close()
					helpers.Error(w, http.StatusInternalServerError, "failed to scan entry collection")
					return
				}
				eCols = append(eCols, col)
			}
			ecRows.Close()
			if err := ecRows.Err(); err != nil {
				helpers.Error(w, http.StatusInternalServerError, "failed to iterate entry collections")
				return
			}
			if eCols == nil {
				eCols = []CollectionItem{}
			}
			entries[j].Collections = eCols
		}

		if entries == nil {
			entries = []EntryItem{}
		}
		chapters[i].Entries = entries
	}

	if chapters == nil {
		chapters = []ChapterItem{}
	}

	resp := map[string]interface{}{
		"chapters": chapters,
	}

	// Store in cache (10 min TTL)
	if encoded, err := json.Marshal(resp); err == nil {
		cache.Set(ctx, cacheKey, string(encoded), 10*time.Minute)
	}

	helpers.JSON(w, http.StatusOK, resp)
}

// ─── PUT /chapters/update/{id} ──────────────────────────

func UpdateChapter(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	chapterID, err := helpers.PathParamInt(r.URL.Path, "/chapters/update/")
	if err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid chapter id")
		return
	}

	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Color       string `json:"color"`
		Collection  []int  `json:"collection"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Update chapter (verify ownership)
	tag, err := db.Pool.Exec(ctx,
		`UPDATE chapters SET title = $1, description = $2, color = $3, updated_at = NOW()
		 WHERE id = $4 AND user_id = $5`,
		req.Title, req.Description, req.Color, chapterID, userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to update chapter")
		return
	}
	if tag.RowsAffected() == 0 {
		helpers.Error(w, http.StatusNotFound, "chapter not found")
		return
	}

	// Replace collections: delete existing, then insert new
	_, err = db.Pool.Exec(ctx,
		`DELETE FROM chapter_collections WHERE chapter_id = $1`,
		chapterID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to update collections")
		return
	}

	for _, colID := range req.Collection {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO chapter_collections (chapter_id, collection_id) VALUES ($1, $2)`,
			chapterID, colID,
		)
		if err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to link collection")
			return
		}
	}

	// Invalidate cache
	cache.DelByPrefix(ctx, "chapters:"+userID)
	cache.DelByPrefix(ctx, "entries:"+userID)

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// ─── DELETE /chapters/delete/{id} ───────────────────────

func DeleteChapter(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	chapterID, err := helpers.PathParamInt(r.URL.Path, "/chapters/delete/")
	if err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid chapter id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM chapters WHERE id = $1 AND user_id = $2`,
		chapterID, userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to delete chapter")
		return
	}
	if tag.RowsAffected() == 0 {
		helpers.Error(w, http.StatusNotFound, "chapter not found")
		return
	}

	// Invalidate cache
	cache.DelByPrefix(ctx, "chapters:"+userID)
	cache.DelByPrefix(ctx, "entries:"+userID)

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// ─── POST /chapters/archive/{id} ────────────────────────

func ArchiveChapter(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	chapterID, err := helpers.PathParamInt(r.URL.Path, "/chapters/archive/")
	if err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid chapter id")
		return
	}

	var req struct {
		IsArchived bool `json:"is_archived"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	tag, err := db.Pool.Exec(ctx,
		`UPDATE chapters SET is_archived = $1, updated_at = NOW()
		 WHERE id = $2 AND user_id = $3`,
		req.IsArchived, chapterID, userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to update chapter")
		return
	}
	if tag.RowsAffected() == 0 {
		helpers.Error(w, http.StatusNotFound, "chapter not found")
		return
	}

	// Invalidate cache
	cache.DelByPrefix(ctx, "chapters:"+userID)
	cache.DelByPrefix(ctx, "entries:"+userID)

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// ─── POST /chapters/mark-favourite/{id} ─────────────────

func MarkFavouriteChapter(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	chapterID, err := helpers.PathParamInt(r.URL.Path, "/chapters/mark-favourite/")
	if err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid chapter id")
		return
	}

	var req struct {
		IsFavourite bool `json:"is_favourite"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	tag, err := db.Pool.Exec(ctx,
		`UPDATE chapters SET is_favourite = $1, updated_at = NOW()
		 WHERE id = $2 AND user_id = $3`,
		req.IsFavourite, chapterID, userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to update chapter")
		return
	}
	if tag.RowsAffected() == 0 {
		helpers.Error(w, http.StatusNotFound, "chapter not found")
		return
	}

	// Invalidate cache
	cache.DelByPrefix(ctx, "chapters:"+userID)
	cache.DelByPrefix(ctx, "entries:"+userID)

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}
