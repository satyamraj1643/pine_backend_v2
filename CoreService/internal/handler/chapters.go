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
	_ = cache.Del(ctx, "collections:"+userID)
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
	cacheKey := "chapters:" + userID + ":edited-v1"
	cached, revision, cacheErr := cache.Read(ctx, cacheKey)
	if cacheErr == nil && cached != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(cached))
		return
	}

	// Fetch chapters
	rows, err := db.Pool.Query(ctx,
		`SELECT id, title, description, color, is_favourite, is_archived, created_at, updated_at,
		 COALESCE((to_jsonb(chapters)->>'edited_at')::timestamptz, updated_at, created_at) AS edit_time
		 FROM chapters
		 WHERE user_id = $1
		 ORDER BY edit_time DESC, id DESC`,
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
		CreatedAt   time.Time        `json:"CreatedAt"`
		UpdatedAt   time.Time        `json:"UpdatedAt"`
		EditedAt    time.Time        `json:"EditedAt"`
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
		EditedAt    time.Time        `json:"EditedAt"`
	}

	var chapters []ChapterItem

	for rows.Next() {
		var ch ChapterItem
		if err := rows.Scan(&ch.ID, &ch.Title, &ch.Description, &ch.Color, &ch.IsFavourite, &ch.IsArchived, &ch.CreatedAt, &ch.UpdatedAt, &ch.EditedAt); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to scan chapter")
			return
		}
		chapters = append(chapters, ch)
	}
	if err := rows.Err(); err != nil {
		helpers.Error(w, http.StatusInternalServerError, "failed to iterate chapters")
		return
	}

	// Load related records in three batched queries, rather than per chapter/entry.
	chapterIndex := make(map[int]int, len(chapters))
	for i := range chapters {
		chapterIndex[chapters[i].ID] = i
		chapters[i].Collections = []CollectionItem{}
		chapters[i].Entries = []EntryItem{}
	}
	if len(chapters) > 0 {
		tags, err := db.Pool.Query(ctx, `SELECT cc.chapter_id, c.id, c.name, c.color
     FROM chapter_collections cc JOIN collections c ON c.id = cc.collection_id
     JOIN chapters ch ON ch.id = cc.chapter_id WHERE ch.user_id = $1 ORDER BY c.id`, userID)
		if err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to fetch chapter tags")
			return
		}
		for tags.Next() {
			var id int
			var tag CollectionItem
			if err := tags.Scan(&id, &tag.ID, &tag.Name, &tag.Color); err != nil {
				tags.Close()
				helpers.Error(w, http.StatusInternalServerError, "failed to scan chapter tags")
				return
			}
			if i, ok := chapterIndex[id]; ok {
				chapters[i].Collections = append(chapters[i].Collections, tag)
			}
		}
		tags.Close()
		if tags.Err() != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to read chapter tags")
			return
		}

		entryRows, err := db.Pool.Query(ctx, `SELECT chapter_id, id, title, content, is_favourite, is_archived, created_at, updated_at,
     COALESCE((to_jsonb(entries)->>'edited_at')::timestamptz, updated_at, created_at) AS edit_time
     FROM entries WHERE user_id = $1 AND chapter_id IS NOT NULL ORDER BY edit_time DESC, id DESC`, userID)
		if err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to fetch entries")
			return
		}
		entryIndex := make(map[int][2]int)
		for entryRows.Next() {
			var chapterID int
			var e EntryItem
			if err := entryRows.Scan(&chapterID, &e.ID, &e.Title, &e.Content, &e.IsFavourite, &e.IsArchived, &e.CreatedAt, &e.UpdatedAt, &e.EditedAt); err != nil {
				entryRows.Close()
				helpers.Error(w, http.StatusInternalServerError, "failed to scan entry")
				return
			}
			if i, ok := chapterIndex[chapterID]; ok {
				e.Collections = []CollectionItem{}
				entryIndex[e.ID] = [2]int{i, len(chapters[i].Entries)}
				chapters[i].Entries = append(chapters[i].Entries, e)
			}
		}
		entryRows.Close()
		if entryRows.Err() != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to read entries")
			return
		}
		entryTags, err := db.Pool.Query(ctx, `SELECT ec.entry_id, c.id, c.name, c.color
     FROM entry_collections ec JOIN collections c ON c.id = ec.collection_id
     JOIN entries e ON e.id = ec.entry_id WHERE e.user_id = $1 AND e.chapter_id IS NOT NULL ORDER BY c.id`, userID)
		if err != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to fetch entry tags")
			return
		}
		for entryTags.Next() {
			var id int
			var tag CollectionItem
			if err := entryTags.Scan(&id, &tag.ID, &tag.Name, &tag.Color); err != nil {
				entryTags.Close()
				helpers.Error(w, http.StatusInternalServerError, "failed to scan entry tags")
				return
			}
			if index, ok := entryIndex[id]; ok {
				item := &chapters[index[0]].Entries[index[1]]
				item.Collections = append(item.Collections, tag)
			}
		}
		entryTags.Close()
		if entryTags.Err() != nil {
			helpers.Error(w, http.StatusInternalServerError, "failed to read entry tags")
			return
		}
	}

	if chapters == nil {
		chapters = []ChapterItem{}
	}

	resp := map[string]interface{}{
		"chapters": chapters,
	}

	// Store in cache (10 min TTL)
	if encoded, err := json.Marshal(resp); err == nil {
		cache.SetIfRevision(ctx, cacheKey, revision, string(encoded), 10*time.Minute)
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
	_ = cache.Del(ctx, "collections:"+userID)
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
	_ = cache.Del(ctx, "collections:"+userID)
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
	_ = cache.Del(ctx, "collections:"+userID)
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
	_ = cache.Del(ctx, "collections:"+userID)
	cache.DelByPrefix(ctx, "entries:"+userID)

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}
