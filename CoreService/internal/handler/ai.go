package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/satyamraj1643/pine_backend_v2/internal/db"
	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
)

// ─── AI Service HTTP Client ─────────────────────────────

var aiHTTPClient = &http.Client{Timeout: 30 * time.Second}

// callAIService sends a POST request to the Python AIService and returns the raw response body.
func callAIService(path string, payload interface{}) ([]byte, int, error) {
	baseURL := os.Getenv("AI_SERVICE_URL")
	baseURL = strings.TrimRight(baseURL, "/")

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal AI payload: %w", err)
	}

	resp, err := aiHTTPClient.Post(baseURL+path, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, 0, fmt.Errorf("AI service request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read AI response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// ─── Helper: strip HTML tags from content ────────────────

func stripHTML(s string) string {
	var buf bytes.Buffer
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			buf.WriteRune(' ')
			continue
		}
		if !inTag {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// ─── Helper: fetch entries for a user ────────────────────

type aiEntryRow struct {
	ID        int
	Title     string
	Content   string
	MoodNames *string
	CreatedAt time.Time
}

func fetchUserEntries(userID string, limit int) ([]aiEntryRow, error) {
	query := `
		SELECT e.id, e.title, e.content,
		       (SELECT STRING_AGG(m.name, ', ') FROM entry_moods em JOIN moods m ON m.id = em.mood_id WHERE em.entry_id = e.id) AS mood_names,
		       e.created_at
		FROM entries e
		WHERE e.user_id = $1 AND e.is_archived = false
		ORDER BY e.created_at DESC
		LIMIT $2
	`
	rows, err := db.Pool.Query(context.Background(), query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []aiEntryRow
	for rows.Next() {
		var e aiEntryRow
		if err := rows.Scan(&e.ID, &e.Title, &e.Content, &e.MoodNames, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// ─── Helper: fetch user's moods list ─────────────────────

type aiMoodRow struct {
	ID    int
	Name  string
	Emoji string
}

func fetchUserMoods(userID string) ([]aiMoodRow, error) {
	rows, err := db.Pool.Query(context.Background(), `SELECT id, name, emoji FROM moods WHERE user_id = $1 ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var moods []aiMoodRow
	for rows.Next() {
		var m aiMoodRow
		if err := rows.Scan(&m.ID, &m.Name, &m.Emoji); err != nil {
			return nil, err
		}
		moods = append(moods, m)
	}
	return moods, nil
}

// ═════════════════════════════════════════════════════════
// 1. POST /ai/reflect — AI reflection on a journal entry
// ═════════════════════════════════════════════════════════

func AIReflect(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid request")
		return
	}

	plainContent := stripHTML(req.Content)
	if len(plainContent) < 10 {
		helpers.Error(w, http.StatusBadRequest, "Write a bit more before asking for a reflection")
		return
	}
	if len(plainContent) > 4000 {
		plainContent = plainContent[:4000]
	}

	respBody, status, err := callAIService("/reflect", map[string]string{
		"title":   req.Title,
		"content": plainContent,
	})
	if err != nil {
		log.Printf("[AI] reflect error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't generate reflection right now")
		return
	}
	if status >= 400 {
		log.Printf("[AI] reflect status %d: %s\n", status, string(respBody))
		helpers.Error(w, http.StatusInternalServerError, "Couldn't generate reflection right now")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}

// ═════════════════════════════════════════════════════════
// 2. POST /ai/suggest-mood — suggest mood from entry text
// ═════════════════════════════════════════════════════════

func AISuggestMood(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid request")
		return
	}

	plainContent := stripHTML(req.Content)
	if len(plainContent) < 10 {
		helpers.Error(w, http.StatusBadRequest, "Write a bit more so I can suggest a mood")
		return
	}
	if len(plainContent) > 3000 {
		plainContent = plainContent[:3000]
	}

	moods, _ := fetchUserMoods(userID)

	payload := map[string]interface{}{
		"content": plainContent,
	}

	if len(moods) > 0 {
		var moodList strings.Builder
		for i, m := range moods {
			if i > 0 {
				moodList.WriteString(", ")
			}
			moodList.WriteString(fmt.Sprintf("%s (id:%d, emoji:%s)", m.Name, m.ID, m.Emoji))
		}
		payload["existing_moods"] = moodList.String()
	}

	respBody, status, err := callAIService("/suggest-mood", payload)
	if err != nil {
		log.Printf("[AI] suggest-mood error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't suggest a mood right now")
		return
	}
	if status >= 400 {
		log.Printf("[AI] suggest-mood status %d: %s\n", status, string(respBody))
		helpers.Error(w, http.StatusInternalServerError, "Couldn't suggest a mood right now")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}

// ═════════════════════════════════════════════════════════
// 3. POST /ai/ask — ask your journal (agentic search)
// ═════════════════════════════════════════════════════════

func AIAsk(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req struct {
		Question string `json:"question"`
	}
	if err := helpers.Decode(r, &req); err != nil || strings.TrimSpace(req.Question) == "" {
		helpers.Error(w, http.StatusBadRequest, "Please ask a question")
		return
	}

	entries, err := fetchUserEntries(userID, 100)
	if err != nil {
		log.Printf("[AI] ask fetch entries error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't load your entries")
		return
	}
	if len(entries) == 0 {
		helpers.Error(w, http.StatusBadRequest, "You don't have any entries yet")
		return
	}

	var entriesContext strings.Builder
	for _, e := range entries {
		plain := stripHTML(e.Content)
		if len(plain) > 500 {
			plain = plain[:500] + "..."
		}
		mood := ""
		if e.MoodNames != nil {
			mood = fmt.Sprintf(" [mood: %s]", *e.MoodNames)
		}
		entriesContext.WriteString(fmt.Sprintf("--- %s (%s)%s ---\n%s\n\n",
			e.Title,
			e.CreatedAt.Format("Jan 2, 2006"),
			mood,
			plain,
		))
	}

	respBody, status, err := callAIService("/ask", map[string]string{
		"question":        req.Question,
		"journal_entries": entriesContext.String(),
	})
	if err != nil {
		log.Printf("[AI] ask error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't process your question right now")
		return
	}
	if status >= 400 {
		log.Printf("[AI] ask status %d: %s\n", status, string(respBody))
		helpers.Error(w, http.StatusInternalServerError, "Couldn't process your question right now")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}

// ═════════════════════════════════════════════════════════
// 4. GET /ai/weekly-recap — AI summary of recent entries
// ═════════════════════════════════════════════════════════

func AIWeeklyRecap(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	query := `
		SELECT e.id, e.title, e.content,
		       (SELECT STRING_AGG(m.name, ', ') FROM entry_moods em JOIN moods m ON m.id = em.mood_id WHERE em.entry_id = e.id) AS mood_names,
		       e.created_at
		FROM entries e
		WHERE e.user_id = $1 AND e.is_archived = false
		  AND e.created_at >= NOW() - INTERVAL '7 days'
		ORDER BY e.created_at ASC
	`
	rows, err := db.Pool.Query(r.Context(), query, userID)
	if err != nil {
		log.Printf("[AI] weekly-recap query error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't load entries")
		return
	}
	defer rows.Close()

	var entries []aiEntryRow
	for rows.Next() {
		var e aiEntryRow
		if err := rows.Scan(&e.ID, &e.Title, &e.Content, &e.MoodNames, &e.CreatedAt); err != nil {
			helpers.Error(w, http.StatusInternalServerError, "Failed reading entries")
			return
		}
		entries = append(entries, e)
	}

	if len(entries) == 0 {
		helpers.JSON(w, http.StatusOK, map[string]interface{}{
			"recap":       "You didn't write anything this week. That's okay — whenever you're ready, your journal is here.",
			"entry_count": 0,
		})
		return
	}

	var ctx strings.Builder
	for _, e := range entries {
		plain := stripHTML(e.Content)
		if len(plain) > 400 {
			plain = plain[:400] + "..."
		}
		mood := ""
		if e.MoodNames != nil {
			mood = fmt.Sprintf(" (feeling: %s)", *e.MoodNames)
		}
		ctx.WriteString(fmt.Sprintf("• %s — \"%s\"%s: %s\n",
			e.CreatedAt.Format("Monday, Jan 2"),
			e.Title,
			mood,
			plain,
		))
	}

	respBody, status, err := callAIService("/weekly-recap", map[string]interface{}{
		"entry_count":    len(entries),
		"weekly_entries": ctx.String(),
	})
	if err != nil {
		log.Printf("[AI] weekly-recap error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't generate recap right now")
		return
	}
	if status >= 400 {
		log.Printf("[AI] weekly-recap status %d: %s\n", status, string(respBody))
		helpers.Error(w, http.StatusInternalServerError, "Couldn't generate recap right now")
		return
	}

	// Merge entry_count into the response
	var result map[string]interface{}
	json.Unmarshal(respBody, &result)
	result["entry_count"] = len(entries)

	helpers.JSON(w, http.StatusOK, result)
}

// ─── Health check for AI ─────────────────────────────────

func AIHealth(w http.ResponseWriter, r *http.Request) {
	respBody, status, err := callAIService("/health", nil)
	if err != nil {
		helpers.JSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"reason":    err.Error(),
		})
		return
	}
	if status >= 400 {
		helpers.JSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"reason":    string(respBody),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}

// ═════════════════════════════════════════════════════════
// 6. GET /ai/insights — structured journal insights
// ═════════════════════════════════════════════════════════

func AIInsights(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entries, err := fetchUserEntries(userID, 60)
	if err != nil {
		log.Printf("[AI] insights fetch error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't load entries")
		return
	}
	if len(entries) < 3 {
		helpers.Error(w, http.StatusBadRequest, "Write a few more entries to see insights")
		return
	}

	var ctx strings.Builder
	for _, e := range entries {
		plain := stripHTML(e.Content)
		if len(plain) > 300 {
			plain = plain[:300] + "..."
		}
		mood := ""
		if e.MoodNames != nil {
			mood = fmt.Sprintf(" [%s]", *e.MoodNames)
		}
		ctx.WriteString(fmt.Sprintf("%s | %s%s | %s\n",
			e.CreatedAt.Format("Jan 2"),
			e.Title,
			mood,
			plain,
		))
	}

	respBody, status, err := callAIService("/insights", map[string]interface{}{
		"entry_count":     len(entries),
		"journal_entries": ctx.String(),
	})
	if err != nil {
		log.Printf("[AI] insights error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't generate insights right now")
		return
	}
	if status >= 400 {
		log.Printf("[AI] insights status %d: %s\n", status, string(respBody))
		helpers.Error(w, http.StatusInternalServerError, "Couldn't generate insights right now")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}

// ═════════════════════════════════════════════════════════
// 7. POST /ai/chat — multi-turn conversation about a note
// ═════════════════════════════════════════════════════════

func AIChat(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)

	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}


	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Messages []struct {
			Role string `json:"role"`
			Text string `json:"text"`
		} `json:"messages"`
	}

	if err := helpers.Decode(r, &req); err != nil {
		helpers.Error(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if len(req.Messages) == 0 {
		helpers.Error(w, http.StatusBadRequest, "No message provided")
		return
	}

	plainContent := stripHTML(req.Content)
	if len(plainContent) > 4000 {
		plainContent = plainContent[:4000]
	}

	// Convert messages to the format the Python service expects
	var messages []map[string]string
	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == "model" || msg.Role == "assistant" {
			role = "assistant"
		}
		messages = append(messages, map[string]string{
			"role":    role,
			"content": msg.Text,
		})
	}

	respBody, status, err := callAIService("/chat", map[string]interface{}{
		"title":    req.Title,
		"content":  plainContent,
		"messages": messages,
	})
	if err != nil {
		log.Printf("[AI] chat error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't reply right now")
		return
	}
	if status >= 400 {
		log.Printf("[AI] chat status %d: %s\n", status, string(respBody))
		helpers.Error(w, http.StatusInternalServerError, "Couldn't reply right now")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}

// ═════════════════════════════════════════════════════════
// 8. GET /ai/personality — writer personality based on journal
// ═════════════════════════════════════════════════════════

func AIPersonality(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entries, err := fetchUserEntries(userID, 80)
	if err != nil {
		log.Printf("[AI] personality fetch error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't load entries")
		return
	}
	if len(entries) < 5 {
		helpers.Error(w, http.StatusBadRequest, "Write a few more entries first — need at least 5")
		return
	}

	var ctx strings.Builder
	for _, e := range entries {
		plain := stripHTML(e.Content)
		if len(plain) > 300 {
			plain = plain[:300] + "..."
		}
		mood := ""
		if e.MoodNames != nil {
			mood = fmt.Sprintf(" [mood: %s]", *e.MoodNames)
		}
		ctx.WriteString(fmt.Sprintf("%s | %s%s | %s\n",
			e.CreatedAt.Format("Jan 2 3:04pm"),
			e.Title,
			mood,
			plain,
		))
	}

	respBody, status, err := callAIService("/personality", map[string]interface{}{
		"entry_count":     len(entries),
		"journal_entries": ctx.String(),
	})
	if err != nil {
		log.Printf("[AI] personality error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't figure out your personality right now")
		return
	}
	if status >= 400 {
		log.Printf("[AI] personality status %d: %s\n", status, string(respBody))
		helpers.Error(w, http.StatusInternalServerError, "Couldn't figure out your personality right now")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}
