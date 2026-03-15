package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/satyamraj1643/pine_backend_v2/internal/db"
	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
	"github.com/satyamraj1643/pine_backend_v2/internal/prompts"
	"github.com/satyamraj1643/pine_backend_v2/internal/tracing"
)

// ─── Bedrock (Claude) types ──────────────────────────────

// Used internally for the chat multi-turn function (same shape as before)
type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

// Claude Messages API types
type claudeMessage struct {
	Role    string        `json:"role"`
	Content []claudeBlock `json:"content"`
}

type claudeBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type claudeRequest struct {
	AnthropicVersion string          `json:"anthropic_version"`
	MaxTokens        int             `json:"max_tokens"`
	Temperature      float64         `json:"temperature,omitempty"`
	System           string          `json:"system,omitempty"`
	Messages         []claudeMessage `json:"messages"`
}

type claudeResponse struct {
	Content    []claudeBlock `json:"content"`
	StopReason string        `json:"stop_reason"`
}

// ─── Bedrock client (lazy init) ──────────────────────────

var brClient *bedrockruntime.Client

func getBedrockClient(ctx context.Context) (*bedrockruntime.Client, error) {
	if brClient != nil {
		return brClient, nil
	}

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("AI not configured — set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY")
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}

	brClient = bedrockruntime.NewFromConfig(cfg)
	return brClient, nil
}

// ─── Bedrock invoke helper (replaces invokeGemini) ───────

var httpClient = &http.Client{Timeout: 30 * time.Second}

func invokeGemini(ctx context.Context, name string, system string, userPrompt string, maxTokens int, temperature float64) (string, error) {
	model := os.Getenv("BEDROCK_MODEL")
	if model == "" {
		model = "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
	}

	span := tracing.StartLLMSpan(name, model, system, userPrompt, "")

	client, err := getBedrockClient(ctx)
	if err != nil {
		tracing.EndSpan(span, "", err)
		return "", err
	}

	reqBody := claudeRequest{
		AnthropicVersion: "bedrock-2023-05-31",
		MaxTokens:        maxTokens,
		Temperature:      temperature,
		System:           system,
		Messages: []claudeMessage{
			{Role: "user", Content: []claudeBlock{{Type: "text", Text: userPrompt}}},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		tracing.EndSpan(span, "", err)
		return "", fmt.Errorf("marshal request: %w", err)
	}

	resp, err := client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(model),
		ContentType: aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		tracing.EndSpan(span, "", err)
		return "", fmt.Errorf("bedrock invoke: %w", err)
	}

	var claudeResp claudeResponse
	if err := json.Unmarshal(resp.Body, &claudeResp); err != nil {
		tracing.EndSpan(span, "", err)
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if len(claudeResp.Content) == 0 {
		emptyErr := fmt.Errorf("empty response from model")
		tracing.EndSpan(span, "", emptyErr)
		return "", emptyErr
	}

	var sb strings.Builder
	for _, block := range claudeResp.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	result := sb.String()
	tracing.EndSpan(span, result, nil)
	return result, nil
}

// Multi-turn conversation invoke (for chat)
func invokeGeminiChat(ctx context.Context, name string, system string, messages []geminiContent, maxTokens int, temperature float64) (string, error) {
	model := os.Getenv("BEDROCK_MODEL")
	if model == "" {
		model = "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
	}

	// Build tracing info — send full conversation history
	var traceMsgs []map[string]string
	for _, msg := range messages {
		role := msg.Role
		if role == "model" {
			role = "assistant"
		}
		text := ""
		if len(msg.Parts) > 0 {
			text = msg.Parts[0].Text
		}
		traceMsgs = append(traceMsgs, map[string]string{"role": role, "content": text})
	}
	span := tracing.StartChatSpan(name, model, system, traceMsgs, "")

	client, err := getBedrockClient(ctx)
	if err != nil {
		tracing.EndSpan(span, "", err)
		return "", err
	}

	// Convert geminiContent messages to Claude messages
	var claudeMessages []claudeMessage
	for _, msg := range messages {
		role := msg.Role
		if role == "model" {
			role = "assistant"
		}
		text := ""
		if len(msg.Parts) > 0 {
			text = msg.Parts[0].Text
		}
		claudeMessages = append(claudeMessages, claudeMessage{
			Role:    role,
			Content: []claudeBlock{{Type: "text", Text: text}},
		})
	}

	reqBody := claudeRequest{
		AnthropicVersion: "bedrock-2023-05-31",
		MaxTokens:        maxTokens,
		Temperature:      temperature,
		System:           system,
		Messages:         claudeMessages,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		tracing.EndSpan(span, "", err)
		return "", fmt.Errorf("marshal request: %w", err)
	}

	resp, err := client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(model),
		ContentType: aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		tracing.EndSpan(span, "", err)
		return "", fmt.Errorf("bedrock invoke: %w", err)
	}

	var claudeResp claudeResponse
	if err := json.Unmarshal(resp.Body, &claudeResp); err != nil {
		tracing.EndSpan(span, "", err)
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if len(claudeResp.Content) == 0 {
		emptyErr := fmt.Errorf("empty response from model")
		tracing.EndSpan(span, "", emptyErr)
		return "", emptyErr
	}

	var sb strings.Builder
	for _, block := range claudeResp.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	result := sb.String()
	tracing.EndSpan(span, result, nil)
	return result, nil
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

func fetchUserEntries(ctx context.Context, userID string, limit int) ([]aiEntryRow, error) {
	query := `
		SELECT e.id, e.title, e.content,
		       (SELECT STRING_AGG(m.name, ', ') FROM entry_moods em JOIN moods m ON m.id = em.mood_id WHERE em.entry_id = e.id) AS mood_names,
		       e.created_at
		FROM entries e
		WHERE e.user_id = $1 AND e.is_archived = false
		ORDER BY e.created_at DESC
		LIMIT $2
	`
	rows, err := db.Pool.Query(ctx, query, userID, limit)
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

func fetchUserMoods(ctx context.Context, userID string) ([]aiMoodRow, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, name, emoji FROM moods WHERE user_id = $1 ORDER BY name`, userID)
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

type reflectReq struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func AIReflect(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req reflectReq
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

	system := prompts.GetSystem("reflect")

	prompt := prompts.FormatHuman("reflect", map[string]string{
		"title":         req.Title,
		"journal_entry": plainContent,
	})

	result, err := invokeGemini(r.Context(), "reflect", system, prompt, 300, 0.8)
	if err != nil {
		log.Printf("[AI] reflect error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't generate reflection right now")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]string{"reflection": strings.TrimSpace(result)})
}

// ═════════════════════════════════════════════════════════
// 2. POST /ai/suggest-mood — suggest mood from entry text
// ═════════════════════════════════════════════════════════

type suggestMoodReq struct {
	Content string `json:"content"`
}

func AISuggestMood(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req suggestMoodReq
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

	moods, _ := fetchUserMoods(r.Context(), userID)

	var system string
	var prompt string

	if len(moods) > 0 {
		var moodList strings.Builder
		for i, m := range moods {
			if i > 0 {
				moodList.WriteString(", ")
			}
			moodList.WriteString(fmt.Sprintf("%s (id:%d, emoji:%s)", m.Name, m.ID, m.Emoji))
		}

		system = prompts.GetSystem("suggest-mood-existing")
		prompt = prompts.FormatHuman("suggest-mood-existing", map[string]string{
			"existing_moods": moodList.String(),
			"journal_entry":  plainContent,
		})
	} else {
		system = prompts.GetSystem("suggest-mood-new")
		prompt = prompts.FormatHuman("suggest-mood-new", map[string]string{
			"journal_entry": plainContent,
		})
	}

	result, err := invokeGemini(r.Context(), "suggest-mood", system, prompt, 100, 0.3)
	if err != nil {
		log.Printf("[AI] suggest-mood error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't suggest a mood right now")
		return
	}

	result = strings.TrimSpace(result)
	result = strings.TrimPrefix(result, "```json")
	result = strings.TrimPrefix(result, "```")
	result = strings.TrimSuffix(result, "```")
	result = strings.TrimSpace(result)

	var suggestion struct {
		MoodID    int    `json:"mood_id"`
		MoodName  string `json:"mood_name"`
		MoodEmoji string `json:"mood_emoji"`
		IsNew     bool   `json:"is_new"`
	}
	if err := json.Unmarshal([]byte(result), &suggestion); err != nil {
		log.Printf("[AI] mood parse error: %v, raw: %s\n", err, result)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't understand AI response")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"mood_id":    suggestion.MoodID,
		"mood_name":  suggestion.MoodName,
		"mood_emoji": suggestion.MoodEmoji,
		"is_new":     suggestion.IsNew,
	})
}

// ═════════════════════════════════════════════════════════
// 3. POST /ai/ask — ask your journal (agentic search)
// ═════════════════════════════════════════════════════════

type askReq struct {
	Question string `json:"question"`
}

func AIAsk(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req askReq
	if err := helpers.Decode(r, &req); err != nil || strings.TrimSpace(req.Question) == "" {
		helpers.Error(w, http.StatusBadRequest, "Please ask a question")
		return
	}

	entries, err := fetchUserEntries(r.Context(), userID, 100)
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

	system := prompts.GetSystem("ask-journal")

	prompt := prompts.FormatHuman("ask-journal", map[string]string{
		"journal_entries": entriesContext.String(),
		"question":        req.Question,
	})

	result, err := invokeGemini(r.Context(), "ask-journal", system, prompt, 500, 0.5)
	if err != nil {
		log.Printf("[AI] ask error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't process your question right now")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]string{"answer": strings.TrimSpace(result)})
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

	system := prompts.GetSystem("weekly-recap")

	prompt := prompts.FormatHuman("weekly-recap", map[string]string{
		"entry_count":    fmt.Sprintf("%d", len(entries)),
		"weekly_entries": ctx.String(),
	})

	result, err := invokeGemini(r.Context(), "weekly-recap", system, prompt, 400, 0.7)
	if err != nil {
		log.Printf("[AI] weekly-recap error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't generate recap right now")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"recap":       strings.TrimSpace(result),
		"entry_count": len(entries),
	})
}

// ─── Health check for AI ─────────────────────────────────

func AIHealth(w http.ResponseWriter, r *http.Request) {
	// Check if AWS Bedrock credentials are configured
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		helpers.JSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"reason":    "AWS credentials not configured",
		})
		return
	}

	_, err := invokeGemini(r.Context(), "health-check", "", "Respond with OK.", 10, 0)
	if err != nil {
		helpers.JSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"reason":    err.Error(),
		})
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]interface{}{
		"available": true,
	})
}

// ═════════════════════════════════════════════════════════
// 6. GET /ai/insights — structured journal insights
// ═════════════════════════════════════════════════════════

type insightTheme struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type insightResponse struct {
	Themes    []insightTheme `json:"themes"`
	Sentiment struct {
		Positive int `json:"positive"`
		Neutral  int `json:"neutral"`
		Negative int `json:"negative"`
	} `json:"sentiment"`
	Patterns []string `json:"patterns"`
	Summary  string   `json:"summary"`
}

func AIInsights(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entries, err := fetchUserEntries(r.Context(), userID, 60)
	if err != nil {
		log.Printf("[AI] insights fetch error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't load entries")
		return
	}
	if len(entries) < 3 {
		helpers.Error(w, http.StatusBadRequest, "Write a few more entries to see insights")
		return
	}

	// Build condensed entry summaries for the LLM
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

	system := prompts.GetSystem("insights")

	prompt := prompts.FormatHuman("insights", map[string]string{
		"entry_count":     fmt.Sprintf("%d", len(entries)),
		"journal_entries": ctx.String(),
	})

	result, err := invokeGemini(r.Context(), "insights", system, prompt, 600, 0.3)
	if err != nil {
		log.Printf("[AI] insights invoke error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't generate insights right now")
		return
	}

	result = strings.TrimSpace(result)
	result = strings.TrimPrefix(result, "```json")
	result = strings.TrimPrefix(result, "```")
	result = strings.TrimSuffix(result, "```")
	result = strings.TrimSpace(result)

	var insights insightResponse
	if err := json.Unmarshal([]byte(result), &insights); err != nil {
		log.Printf("[AI] insights parse error: %v, raw: %s\n", err, result)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't understand the analysis")
		return
	}

	helpers.JSON(w, http.StatusOK, insights)
}

// ═════════════════════════════════════════════════════════
// 7. POST /ai/chat — multi-turn conversation about a note
// ═════════════════════════════════════════════════════════

type chatMessage struct {
	Role string `json:"role"` // "user" or "model"
	Text string `json:"text"`
}

type chatReq struct {
	Title    string        `json:"title"`
	Content  string        `json:"content"`
	Messages []chatMessage `json:"messages"`
}

func AIChat(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req chatReq
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

	// The chat system prompt has {title} and {journal_entry} variables baked in
	baseSystem := prompts.GetSystem("chat")
	system := strings.ReplaceAll(baseSystem, "{title}", req.Title)
	system = strings.ReplaceAll(system, "{journal_entry}", plainContent)

	// Build Gemini conversation history
	var geminiMessages []geminiContent
	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == "model" || msg.Role == "assistant" {
			role = "model"
		}
		geminiMessages = append(geminiMessages, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.Text}},
		})
	}

	result, err := invokeGeminiChat(r.Context(), "chat", system, geminiMessages, 300, 0.85)
	if err != nil {
		log.Printf("[AI] chat error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't reply right now")
		return
	}

	helpers.JSON(w, http.StatusOK, map[string]string{"reply": strings.TrimSpace(result)})
}

// ═════════════════════════════════════════════════════════
// 8. GET /ai/personality — writer personality based on journal
// ═════════════════════════════════════════════════════════

type personalityResponse struct {
	Archetype string   `json:"archetype"`
	Summary   string   `json:"summary"`
	Traits    []string `json:"traits"`
	Vibes     []string `json:"vibes"`
	Energy    string   `json:"energy"`
	Patterns  []string `json:"patterns"`
}

func AIPersonality(w http.ResponseWriter, r *http.Request) {
	userID := helpers.GetUserID(r)
	if userID == "" {
		helpers.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entries, err := fetchUserEntries(r.Context(), userID, 80)
	if err != nil {
		log.Printf("[AI] personality fetch error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't load entries")
		return
	}
	if len(entries) < 5 {
		helpers.Error(w, http.StatusBadRequest, "Write a few more entries first — need at least 5")
		return
	}

	// Build condensed entry context
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

	system := prompts.GetSystem("personality")

	prompt := prompts.FormatHuman("personality", map[string]string{
		"entry_count":     fmt.Sprintf("%d", len(entries)),
		"journal_entries": ctx.String(),
	})

	result, err := invokeGemini(r.Context(), "personality", system, prompt, 800, 0.85)
	if err != nil {
		log.Printf("[AI] personality invoke error: %v\n", err)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't figure out your personality right now")
		return
	}

	result = strings.TrimSpace(result)
	result = strings.TrimPrefix(result, "```json")
	result = strings.TrimPrefix(result, "```")
	result = strings.TrimSuffix(result, "```")
	result = strings.TrimSpace(result)

	var personality personalityResponse
	if err := json.Unmarshal([]byte(result), &personality); err != nil {
		log.Printf("[AI] personality parse error: %v, raw: %s\n", err, result)
		helpers.Error(w, http.StatusInternalServerError, "Couldn't understand the analysis")
		return
	}

	helpers.JSON(w, http.StatusOK, personality)
}
