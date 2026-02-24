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
		model = "us.anthropic.claude-3-5-sonnet-20241022-v2:0"
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
		model = "us.anthropic.claude-3-5-sonnet-20241022-v2:0"
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

	system := `You are a warm, empathetic journal companion for a young person. You just read their journal entry. Give a brief, thoughtful reflection (2-4 sentences max). Be genuine, not preachy. Acknowledge their feelings without being a therapist. Talk like a supportive best friend who really gets it. Never use bullet points or lists — just natural, flowing text. Don't start with "It sounds like" or "I notice that".`

	prompt := fmt.Sprintf("Here is the journal entry:\n\nTitle: %s\n\n%s", req.Title, plainContent)

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
		// Has existing moods — try to match one, or suggest a new one if nothing fits
		var moodList strings.Builder
		for i, m := range moods {
			if i > 0 {
				moodList.WriteString(", ")
			}
			moodList.WriteString(fmt.Sprintf("%s (id:%d, emoji:%s)", m.Name, m.ID, m.Emoji))
		}

		system = `You analyze a journal entry to detect the writer's mood. You MUST respond with ONLY valid JSON, no other text.

IMPORTANT: You MUST pick from the user's existing moods. Be generous — if an existing mood is even a rough fit, use it. Interpret mood names broadly. For example "happy" covers joy, excitement, contentment; "sad" covers melancholy, disappointment, longing; "calm" covers peaceful, relaxed, content.

Return an existing mood:
{"mood_id": <integer>, "mood_name": "<string>", "mood_emoji": "<their existing emoji>", "is_new": false}

ONLY if absolutely NONE of the existing moods are even a loose fit for the entry's tone, return a new mood:
{"mood_id": 0, "mood_name": "<new mood name>", "mood_emoji": "<bare shortcode>", "is_new": true}

Rules for new moods (last resort only):
- Name should be a single lowercase word like "nostalgic", "grateful", "restless", "hopeful"
- Emoji shortcode must be bare (no colons). Examples: smile, pensive, relieved, heart, grinning. NOT :smile: or :pensive:
- You should almost never need to suggest a new mood if the user has 3+ existing moods`

		prompt = fmt.Sprintf("Existing moods: %s\n\nJournal entry:\n%s\n\nRespond with JSON only.", moodList.String(), plainContent)
	} else {
		// No existing moods — suggest a fresh one
		system = `You analyze a journal entry to detect the writer's mood. You MUST respond with ONLY valid JSON, no other text.

Return:
{"mood_id": 0, "mood_name": "<mood name>", "mood_emoji": "<bare shortcode>", "is_new": true}

Rules:
- Name should be a single lowercase word like "happy", "anxious", "calm", "grateful", "restless"
- Emoji shortcode must be bare (no colons). Examples: smile, pensive, relieved, heart, grinning. NOT :smile: or :pensive:
- Pick the single best mood that captures the overall tone of the entry`

		prompt = fmt.Sprintf("Journal entry:\n%s\n\nRespond with JSON only.", plainContent)
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

	system := `You are a helpful journal assistant. The user is asking a question about their past journal entries. Answer based ONLY on the entries provided — don't make things up. Be specific: mention dates and entry titles when relevant. If you can't find the answer in the entries, say so honestly. Keep it conversational, warm, and brief (3-5 sentences). Never reveal raw data formats or IDs.`

	prompt := fmt.Sprintf("Here are my journal entries:\n\n%s\n\nMy question: %s", entriesContext.String(), req.Question)

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

	system := `You are a warm journal companion. Write a brief weekly recap (3-4 sentences) summarizing the user's journal entries from this week. Mention key themes, mood shifts, and highlights. Address the user directly ("You..." / "Your week..."). Keep it casual, genuine, and supportive. Don't use bullet points or lists.`

	prompt := fmt.Sprintf("Here are my entries from the past week (%d total):\n\n%s", len(entries), ctx.String())

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
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		helpers.JSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"reason":    "GEMINI_API_KEY not set",
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

	system := `You analyze journal entries and return ONLY valid JSON. No other text, no markdown fences, just the raw JSON object.

The JSON must match this exact structure:
{
  "themes": [{"name": "theme name", "count": number}],
  "sentiment": {"positive": number, "neutral": number, "negative": number},
  "patterns": ["pattern observation 1", "pattern observation 2", "pattern observation 3"],
  "summary": "one sentence overall summary"
}

Rules:
- themes: Extract 5-8 recurring topics. "count" = how many entries mention that topic. Use lowercase single-word or two-word labels (e.g. "work", "relationships", "self care", "family", "fitness").
- sentiment: Percentage breakdown of entries by tone. Must sum to 100.
- patterns: 2-4 short behavioral observations. Be specific and useful, not generic. Example: "You write longer entries when stressed" or "Weekends tend to be more positive". Write in second person.
- summary: One casual sentence summarizing the journal. Address the user as "you".`

	prompt := fmt.Sprintf("Analyze these %d journal entries:\n\n%s", len(entries), ctx.String())

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

	system := fmt.Sprintf(`You are the user's journal buddy — a warm, thoughtful friend they can talk to about their journal entries. You've just read this entry:

Title: %s

%s

Rules:
- Talk like a close friend, not a therapist or an AI assistant
- Keep replies short (2-4 sentences usually). Be concise but genuine
- You can ask follow-up questions to understand how they feel
- Never be preachy or give unsolicited advice unless they ask
- Reference specific things from their entry to show you actually read it
- Match their energy — if they're casual, be casual. If they're serious, be thoughtful
- Never say "I'm an AI" or "As an AI". You're their buddy`, req.Title, plainContent)

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

	system := `You are a personality analyst for a journaling app. You read someone's journal entries and figure out their writer personality. You speak in a casual, warm, gen-z friendly tone — like a smart friend who gets them.

Return ONLY valid JSON. No markdown fences, no extra text. Just the raw JSON object matching this exact structure:

{
  "archetype": "string",
  "summary": "string",
  "traits": ["string"],
  "vibes": ["string"],
  "energy": "string",
  "patterns": ["string"]
}

Rules:
- archetype: A creative 2-4 word name for their writer personality. Make it feel like a character class or zodiac archetype. Examples: "The Midnight Philosopher", "Chaos Poet", "The Gentle Observer", "Sunset Overthinker", "The Quiet Storm". Be creative and specific to THEM, not generic.
- summary: A casual 2-3 sentence paragraph describing who they are as a writer. Use "you" language. Should feel like a friend describing them, not a psych evaluation. Reference specific patterns you noticed.
- traits: 4-6 single-word or two-word personality traits based on their writing style and content. Lowercase. Examples: "introspective", "emotionally honest", "detail-oriented", "restless", "grounded".
- vibes: 3-5 casual one-liner observations that start with "you" or "the type who". Should feel relatable and slightly funny. Examples: "you journal at 2am and call it self-care", "the type who re-reads old entries like they're love letters to yourself".
- energy: One word describing their overall energy. Pick from: calm, dreamy, intense, chaotic, warm, bold, quiet, restless, grounded, electric. Choose the single most fitting one.
- patterns: 2-4 specific behavioral patterns you noticed in their writing. Be concrete, not generic. Examples: "You write more when you're anxious — your entries get longer and more detailed", "Weekends are your most reflective days". Use second person "you".`

	prompt := fmt.Sprintf("Here are %d journal entries from this person. Figure out their writer personality:\n\n%s", len(entries), ctx.String())

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
