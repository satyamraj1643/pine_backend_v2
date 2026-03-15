package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/satyamraj1643/pine_backend_v2/internal/cache"
	"github.com/satyamraj1643/pine_backend_v2/internal/db"
	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
)

// ─── Request / Response types ────────────────────────────

type createEntryReq struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	Chapter    *int   `json:"chapter"`
	Mood       []int  `json:"mood"`
	Collection []int  `json:"collection"`
}

type updateEntryReq struct {
	Title      *string `json:"title"`
	Content    *string `json:"content"`
	Chapter    *int    `json:"chapter"`
	Mood       *[]int  `json:"mood"`
	Collection *[]int  `json:"collection"`
}

type archiveReq struct {
	IsArchived bool `json:"is_archived"`
}

type favouriteReq struct {
	IsFavourite bool `json:"is_favourite"`
}

type chapterDTO struct {
	ID    int    `json:"ID"`
	Title string `json:"Title"`
	Color string `json:"Color"`
}

type moodDTO struct {
	ID    int    `json:"ID"`
	Name  string `json:"Name"`
	Emoji string `json:"Emoji"`
	Color string `json:"Color"`
}

type collectionDTO struct {
	ID    int    `json:"ID"`
	Name  string `json:"Name"`
	Color string `json:"Color"`
}

type entryDTO struct {
	ID          int             `json:"ID"`
	Title       string          `json:"Title"`
	Content     string          `json:"Content"`
	Chapter     *chapterDTO     `json:"Chapter"`
	Moods       []moodDTO       `json:"Moods"`
	Collections []collectionDTO `json:"Collections"`
	IsFavourite bool            `json:"IsFavourite"`
	IsArchived  bool            `json:"IsArchived"`
	CreatedAt   time.Time       `json:"CreatedAt"`
	UpdatedAt   time.Time       `json:"UpdatedAt"`
}

// ─── 1. POST /entries/create-new ─────────────────────────

func CreateEntry(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req createEntryReq
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx := r.Context()

	var entryID int
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO entries (user_id, title, content, chapter_id, is_favourite, is_archived, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, false, false, NOW(), NOW())
		 RETURNING id`,
		userID, req.Title, req.Content, req.Chapter,
	).Scan(&entryID)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "Failed to create entry")
		return
	}

	if len(req.Mood) > 0 {
		if err := insertEntryMoods(ctx, entryID, req.Mood); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "Failed to link moods")
			return
		}
	}

	if len(req.Collection) > 0 {
		if err := insertEntryCollections(ctx, entryID, req.Collection); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "Failed to link collections")
			return
		}
	}

	_ = cache.DelByPrefix(ctx, "entries:"+userID)
	_ = cache.DelByPrefix(ctx, "chapters:"+userID)

	helpers.JSON(w, http.StatusCreated, map[string]interface{}{"created": true, "id": entryID})
}

// ─── 2. GET /entries/all ─────────────────────────────────

func GetAllEntries(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	ctx := r.Context()
	cacheKey := "entries:" + userID

	// Check cache first.
	if cached, err := cache.Get(ctx, cacheKey); err == nil && cached != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(cached))
		return
	}

	// Query entries with optional chapter via LEFT JOIN.
	rows, err := db.Pool.Query(ctx,
		`SELECT
			e.id, e.title, e.content,
			ch.id, ch.title, ch.color,
			e.is_favourite, e.is_archived, e.created_at, e.updated_at
		 FROM entries e
		 LEFT JOIN chapters ch ON ch.id = e.chapter_id
		 WHERE e.user_id = $1
		 ORDER BY e.updated_at DESC`,
		userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "Failed to fetch entries")
		return
	}
	defer rows.Close()

	entries := make([]entryDTO, 0)

	for rows.Next() {
		var e entryDTO
		var chID *int
		var chTitle, chColor *string

		if err := rows.Scan(
			&e.ID, &e.Title, &e.Content,
			&chID, &chTitle, &chColor,
			&e.IsFavourite, &e.IsArchived, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "Failed to scan entry")
			return
		}

		if chID != nil {
			e.Chapter = &chapterDTO{ID: *chID, Title: *chTitle, Color: *chColor}
		}

		e.Moods = make([]moodDTO, 0)
		e.Collections = make([]collectionDTO, 0)
		entries = append(entries, e)
	}
	if rows.Err() != nil {
		helpers.Error(w, http.StatusInternalServerError, "Failed to iterate entries")
		return
	}

	// Fetch moods and collections for all entries in batch queries.
	if len(entries) > 0 {
		entryIDs := make([]int, len(entries))
		idxMap := make(map[int]int, len(entries)) // entry_id -> slice index
		for i, e := range entries {
			entryIDs[i] = e.ID
			idxMap[e.ID] = i
		}

		// Batch fetch moods via entry_moods join table.
		moodRows, err := db.Pool.Query(ctx,
			`SELECT em.entry_id, m.id, m.name, m.emoji, m.color
			 FROM entry_moods em
			 JOIN moods m ON m.id = em.mood_id
			 WHERE em.entry_id = ANY($1)`,
			entryIDs,
		)
		if err == nil {
			defer moodRows.Close()
			for moodRows.Next() {
				var entryID, mID int
				var mName, mEmoji, mColor string
				if err := moodRows.Scan(&entryID, &mID, &mName, &mEmoji, &mColor); err == nil {
					if idx, ok := idxMap[entryID]; ok {
						entries[idx].Moods = append(entries[idx].Moods, moodDTO{
							ID: mID, Name: mName, Emoji: mEmoji, Color: mColor,
						})
					}
				}
			}
		}

		// Batch fetch collections via entry_collections join table.
		colRows, err := db.Pool.Query(ctx,
			`SELECT ec.entry_id, c.id, c.name, c.color
			 FROM entry_collections ec
			 JOIN collections c ON c.id = ec.collection_id
			 WHERE ec.entry_id = ANY($1)`,
			entryIDs,
		)
		if err == nil {
			defer colRows.Close()
			for colRows.Next() {
				var entryID, colID int
				var colName, colColor string
				if err := colRows.Scan(&entryID, &colID, &colName, &colColor); err == nil {
					if idx, ok := idxMap[entryID]; ok {
						entries[idx].Collections = append(entries[idx].Collections, collectionDTO{
							ID: colID, Name: colName, Color: colColor,
						})
					}
				}
			}
		}
	}

	resp := map[string]interface{}{"data": entries}
	data, err := json.Marshal(resp)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "Failed to marshal response")
		return
	}

	// Store in cache with 5 min TTL.
	_ = cache.Set(ctx, cacheKey, string(data), 5*time.Minute)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// ─── 3. PATCH /entries/details/{id} ──────────────────────

func UpdateEntry(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entryID, err := helpers.PathParamInt(r.URL.Path, "/entries/details/")
	if err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid entry ID")
		return
	}

	var req updateEntryReq
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx := r.Context()

	// Build dynamic SET clause.
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.Title != nil {
		setClauses = append(setClauses, pgArg("title", &argIdx))
		args = append(args, *req.Title)
	}
	if req.Content != nil {
		setClauses = append(setClauses, pgArg("content", &argIdx))
		args = append(args, *req.Content)
	}
	if req.Chapter != nil {
		setClauses = append(setClauses, pgArg("chapter_id", &argIdx))
		args = append(args, *req.Chapter)
	}

	// Always bump updated_at.
	setClauses = append(setClauses, pgArg("updated_at", &argIdx))
	args = append(args, time.Now())

	query := "UPDATE entries SET " + strings.Join(setClauses, ", ") +
		" WHERE id = $" + pgIdx(&argIdx) + " AND user_id = $" + pgIdx(&argIdx)
	args = append(args, entryID, userID)

	tag, err := db.Pool.Exec(ctx, query, args...)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "Failed to update entry")
		return
	}
	if tag.RowsAffected() == 0 {
		helpers.Error(w, http.StatusNotFound, "Entry not found")
		return
	}

	// Replace moods if provided.
	if req.Mood != nil {
		if err := replaceEntryMoods(ctx, entryID, *req.Mood); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "Failed to update moods")
			return
		}
	}

	// Replace collections if provided.
	if req.Collection != nil {
		if err := replaceEntryCollections(ctx, entryID, *req.Collection); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "Failed to update collections")
			return
		}
	}

	_ = cache.DelByPrefix(ctx, "entries:"+userID)
	_ = cache.DelByPrefix(ctx, "chapters:"+userID)

	helpers.JSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ─── 4. DELETE /entries/delete/{id} ──────────────────────

func DeleteEntry(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entryID, err := helpers.PathParamInt(r.URL.Path, "/entries/delete/")
	if err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid entry ID")
		return
	}

	ctx := r.Context()

	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM entries WHERE id = $1 AND user_id = $2`,
		entryID, userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "Failed to delete entry")
		return
	}
	if tag.RowsAffected() == 0 {
		helpers.Error(w, http.StatusNotFound, "Entry not found")
		return
	}

	_ = cache.DelByPrefix(ctx, "entries:"+userID)
	_ = cache.DelByPrefix(ctx, "chapters:"+userID)

	w.WriteHeader(http.StatusNoContent)
}

// ─── 5. POST /entries/archive/{id} ──────────────────────

func ArchiveEntry(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entryID, err := helpers.PathParamInt(r.URL.Path, "/entries/archive/")
	if err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid entry ID")
		return
	}

	var req archiveReq
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx := r.Context()

	tag, err := db.Pool.Exec(ctx,
		`UPDATE entries SET is_archived = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`,
		req.IsArchived, entryID, userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "Failed to update archive status")
		return
	}
	if tag.RowsAffected() == 0 {
		helpers.Error(w, http.StatusNotFound, "Entry not found")
		return
	}

	_ = cache.DelByPrefix(ctx, "entries:"+userID)
	_ = cache.DelByPrefix(ctx, "chapters:"+userID)

	helpers.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ─── 6. POST /entries/mark-favourite/{id} ────────────────

func MarkFavouriteEntry(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entryID, err := helpers.PathParamInt(r.URL.Path, "/entries/mark-favourite/")
	if err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid entry ID")
		return
	}

	var req favouriteReq
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx := r.Context()

	tag, err := db.Pool.Exec(ctx,
		`UPDATE entries SET is_favourite = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`,
		req.IsFavourite, entryID, userID,
	)
	if err != nil {
		helpers.Error(w, http.StatusInternalServerError, "Failed to update favourite status")
		return
	}
	if tag.RowsAffected() == 0 {
		helpers.Error(w, http.StatusNotFound, "Entry not found")
		return
	}

	_ = cache.DelByPrefix(ctx, "entries:"+userID)
	_ = cache.DelByPrefix(ctx, "chapters:"+userID)

	helpers.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ─── Helpers ─────────────────────────────────────────────

// insertEntryMoods inserts rows into the entry_moods junction table.
func insertEntryMoods(ctx context.Context, entryID int, moodIDs []int) error {
	for _, mID := range moodIDs {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO entry_moods (entry_id, mood_id) VALUES ($1, $2)`,
			entryID, mID,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// replaceEntryMoods deletes existing links and inserts new ones.
func replaceEntryMoods(ctx context.Context, entryID int, moodIDs []int) error {
	_, err := db.Pool.Exec(ctx,
		`DELETE FROM entry_moods WHERE entry_id = $1`,
		entryID,
	)
	if err != nil {
		return err
	}
	return insertEntryMoods(ctx, entryID, moodIDs)
}

// insertEntryCollections inserts rows into the entry_collections junction table.
func insertEntryCollections(ctx context.Context, entryID int, collectionIDs []int) error {
	for _, colID := range collectionIDs {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO entry_collections (entry_id, collection_id) VALUES ($1, $2)`,
			entryID, colID,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// replaceEntryCollections deletes existing links and inserts new ones.
func replaceEntryCollections(ctx context.Context, entryID int, collectionIDs []int) error {
	_, err := db.Pool.Exec(ctx,
		`DELETE FROM entry_collections WHERE entry_id = $1`,
		entryID,
	)
	if err != nil {
		return err
	}
	return insertEntryCollections(ctx, entryID, collectionIDs)
}

// pgArg returns a "column = $N" fragment and increments the counter.
func pgArg(col string, idx *int) string {
	s := col + " = $" + itoa(*idx)
	*idx++
	return s
}

// pgIdx returns the current index as a string and increments the counter.
func pgIdx(idx *int) string {
	s := itoa(*idx)
	*idx++
	return s
}

// itoa is a tiny int-to-string helper to avoid importing strconv.
func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}
