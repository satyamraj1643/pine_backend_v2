package prompts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ─── Types ───────────────────────────────────────────────

// Prompt holds both the system instructions and the human message template.
type Prompt struct {
	Name          string   // LangSmith repo handle, e.g. "pine-reflect"
	System        string   // System message (AI instructions)
	HumanTemplate string   // Human message template with {named_variables}
	InputVars     []string // Variable names used in HumanTemplate
}

type cachedPrompt struct {
	system    string
	human     string
	fetchedAt time.Time
}

// ─── State ───────────────────────────────────────────────

var (
	registry map[string]Prompt
	cache    map[string]*cachedPrompt
	mu       sync.RWMutex
	apiKey   string
	hubBase  = "https://api.smith.langchain.com/api/v1"
	httpCl   = &http.Client{Timeout: 15 * time.Second}
	ttl      = 5 * time.Minute
	hubReady bool
)

// ─── Default prompts ────────────────────────────────────

func defaultPrompts() map[string]Prompt {
	return map[string]Prompt{
		"reflect": {
			Name:   "pine-reflect",
			System: `You are a warm, empathetic journal companion for a young person. You just read their journal entry. Give a brief, thoughtful reflection (2-4 sentences max). Be genuine, not preachy. Acknowledge their feelings without being a therapist. Talk like a supportive best friend who really gets it. Never use bullet points or lists — just natural, flowing text. Don't start with "It sounds like" or "I notice that".`,
			HumanTemplate: `Here is the journal entry:

Title: {title}

{journal_entry}`,
			InputVars: []string{"title", "journal_entry"},
		},
		"suggest-mood-existing": {
			Name: "pine-suggest-mood-existing",
			System: `You analyze a journal entry to detect the writer's mood. You MUST respond with ONLY valid JSON, no other text.

IMPORTANT: You MUST pick from the user's existing moods. Be generous — if an existing mood is even a rough fit, use it. Interpret mood names broadly. For example "happy" covers joy, excitement, contentment; "sad" covers melancholy, disappointment, longing; "calm" covers peaceful, relaxed, content.

Return an existing mood:
{"mood_id": <integer>, "mood_name": "<string>", "mood_emoji": "<their existing emoji>", "is_new": false}

ONLY if absolutely NONE of the existing moods are even a loose fit for the entry's tone, return a new mood:
{"mood_id": 0, "mood_name": "<new mood name>", "mood_emoji": "<bare shortcode>", "is_new": true}

Rules for new moods (last resort only):
- Name should be a single lowercase word like "nostalgic", "grateful", "restless", "hopeful"
- Emoji shortcode must be bare (no colons). Examples: smile, pensive, relieved, heart, grinning. NOT :smile: or :pensive:
- You should almost never need to suggest a new mood if the user has 3+ existing moods`,
			HumanTemplate: `Existing moods: {existing_moods}

Journal entry:
{journal_entry}

Respond with JSON only.`,
			InputVars: []string{"existing_moods", "journal_entry"},
		},
		"suggest-mood-new": {
			Name: "pine-suggest-mood-new",
			System: `You analyze a journal entry to detect the writer's mood. You MUST respond with ONLY valid JSON, no other text.

Return:
{"mood_id": 0, "mood_name": "<mood name>", "mood_emoji": "<bare shortcode>", "is_new": true}

Rules:
- Name should be a single lowercase word like "happy", "anxious", "calm", "grateful", "restless"
- Emoji shortcode must be bare (no colons). Examples: smile, pensive, relieved, heart, grinning. NOT :smile: or :pensive:
- Pick the single best mood that captures the overall tone of the entry`,
			HumanTemplate: `Journal entry:
{journal_entry}

Respond with JSON only.`,
			InputVars: []string{"journal_entry"},
		},
		"ask-journal": {
			Name:   "pine-ask-journal",
			System: `You are a helpful journal assistant. The user is asking a question about their past journal entries. Answer based ONLY on the entries provided — don't make things up. Be specific: mention dates and entry titles when relevant. If you can't find the answer in the entries, say so honestly. Keep it conversational, warm, and brief (3-5 sentences). Never reveal raw data formats or IDs.`,
			HumanTemplate: `Here are my journal entries:

{journal_entries}

My question: {question}`,
			InputVars: []string{"journal_entries", "question"},
		},
		"weekly-recap": {
			Name:   "pine-weekly-recap",
			System: `You are a warm journal companion. Write a brief weekly recap (3-4 sentences) summarizing the user's journal entries from this week. Mention key themes, mood shifts, and highlights. Address the user directly ("You..." / "Your week..."). Keep it casual, genuine, and supportive. Don't use bullet points or lists.`,
			HumanTemplate: `Here are my entries from the past week ({entry_count} total):

{weekly_entries}`,
			InputVars: []string{"entry_count", "weekly_entries"},
		},
		"insights": {
			Name: "pine-insights",
			System: `You analyze journal entries and return ONLY valid JSON. No other text, no markdown fences, just the raw JSON object.

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
- summary: One casual sentence summarizing the journal. Address the user as "you".`,
			HumanTemplate: `Analyze these {entry_count} journal entries:

{journal_entries}`,
			InputVars: []string{"entry_count", "journal_entries"},
		},
		"chat": {
			Name: "pine-chat",
			System: `You are the user's journal buddy — a warm, thoughtful friend they can talk to about their journal entries.

Rules:
- Talk like a close friend, not a therapist or an AI assistant
- Keep replies short (2-4 sentences usually). Be concise but genuine
- You can ask follow-up questions to understand how they feel
- Never be preachy or give unsolicited advice unless they ask
- Reference specific things from their entry to show you actually read it
- Match their energy — if they're casual, be casual. If they're serious, be thoughtful
- Never say "I'm an AI" or "As an AI". You're their buddy

The user's journal entry for context:

Title: {title}

{journal_entry}`,
			HumanTemplate: `{message}`,
			InputVars:     []string{"title", "journal_entry", "message"},
		},
		"personality": {
			Name: "pine-personality",
			System: `You are a personality analyst for a journaling app. You read someone's journal entries and figure out their writer personality. You speak in a casual, warm, gen-z friendly tone — like a smart friend who gets them.

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
- patterns: 2-4 specific behavioral patterns you noticed in their writing. Be concrete, not generic. Examples: "You write more when you're anxious — your entries get longer and more detailed", "Weekends are your most reflective days". Use second person "you".`,
			HumanTemplate: `Here are {entry_count} journal entries from this person. Figure out their writer personality:

{journal_entries}`,
			InputVars: []string{"entry_count", "journal_entries"},
		},
	}
}

// ─── Init ────────────────────────────────────────────────

func Init() {
	apiKey = os.Getenv("LANGSMITH_API_KEY")
	cache = make(map[string]*cachedPrompt)
	registry = defaultPrompts()

	if apiKey == "" {
		log.Println("[Prompts] LangSmith not configured — using default prompts")
		return
	}

	hubReady = true
	log.Printf("[Prompts] LangSmith Hub enabled — %d prompts registered\n", len(registry))

	// Push defaults to Hub in background (idempotent)
	go pushAllToHub()
}

// ─── Public API ─────────────────────────────────────────

// GetSystem returns the system prompt for a given key.
// Pulls from LangSmith Hub if available (cached 5 min), falls back to defaults.
func GetSystem(name string) string {
	sys, _ := getFromCache(name)
	if sys != "" {
		return sys
	}

	// Try pulling from Hub
	if hubReady {
		if p, ok := registry[name]; ok {
			if sys, human, err := pullFromHub(p.Name); err == nil && sys != "" {
				mu.Lock()
				cache[name] = &cachedPrompt{system: sys, human: human, fetchedAt: time.Now()}
				mu.Unlock()
				return sys
			}
		}
	}

	// Fall back to default
	if p, ok := registry[name]; ok {
		return p.System
	}
	return ""
}

// GetHuman returns the human message template for a given key.
func GetHuman(name string) string {
	_, human := getFromCache(name)
	if human != "" {
		return human
	}

	// Try pulling from Hub
	if hubReady {
		if p, ok := registry[name]; ok {
			if sys, human, err := pullFromHub(p.Name); err == nil && human != "" {
				mu.Lock()
				cache[name] = &cachedPrompt{system: sys, human: human, fetchedAt: time.Now()}
				mu.Unlock()
				return human
			}
		}
	}

	// Fall back to default
	if p, ok := registry[name]; ok {
		return p.HumanTemplate
	}
	return ""
}

// FormatHuman renders the human template for a given prompt with the provided variables.
// Variable placeholders use {name} syntax (LangChain f-string format).
func FormatHuman(name string, vars map[string]string) string {
	tpl := GetHuman(name)
	if tpl == "" {
		return ""
	}
	for k, v := range vars {
		tpl = strings.ReplaceAll(tpl, "{"+k+"}", v)
	}
	return tpl
}

// getFromCache returns cached system and human templates if they exist and are fresh.
func getFromCache(name string) (string, string) {
	mu.RLock()
	defer mu.RUnlock()
	if c, ok := cache[name]; ok && time.Since(c.fetchedAt) < ttl {
		return c.system, c.human
	}
	return "", ""
}

// ─── Hub operations ─────────────────────────────────────

func pushAllToHub() {
	for key, p := range registry {
		if err := ensureRepoExists(p.Name, key); err != nil {
			log.Printf("[Prompts] Hub repo '%s': %v\n", p.Name, err)
			continue
		}
		if err := commitPrompt(p); err != nil {
			log.Printf("[Prompts] Hub commit '%s': %v\n", p.Name, err)
		} else {
			log.Printf("[Prompts] Hub pushed '%s'\n", p.Name)
		}
	}
	log.Println("[Prompts] All prompts pushed to LangSmith Hub")
}

// ensureRepoExists creates the prompt repo if it doesn't exist.
func ensureRepoExists(repoHandle string, description string) error {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/repos/-/%s", hubBase, repoHandle), nil)
	req.Header.Set("x-api-key", apiKey)
	resp, err := httpCl.Do(req)
	if err != nil {
		return fmt.Errorf("check repo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return nil
	}

	body, _ := json.Marshal(map[string]interface{}{
		"repo_handle": repoHandle,
		"description": fmt.Sprintf("Pine Journal prompt: %s", description),
		"is_public":   false,
	})

	req, _ = http.NewRequest("POST", hubBase+"/repos", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	resp2, err := httpCl.Do(req)
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode >= 400 {
		b, _ := io.ReadAll(resp2.Body)
		return fmt.Errorf("create repo %d: %s", resp2.StatusCode, string(b))
	}
	return nil
}

// commitPrompt pushes a prompt version to the Hub.
// Includes parent_commit hash for proper version chaining.
func commitPrompt(p Prompt) error {
	manifest := buildChatPromptManifest(p.System, p.HumanTemplate, p.InputVars)

	payload := map[string]interface{}{
		"manifest": manifest,
	}

	if hash, err := getLatestCommitHash(p.Name); err == nil && hash != "" {
		payload["parent_commit"] = hash
	}

	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/commits/-/%s", hubBase, p.Name), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	resp, err := httpCl.Do(req)
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("commit %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// getLatestCommitHash returns the latest commit hash for a repo, or "" if none.
func getLatestCommitHash(repoHandle string) (string, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/commits/-/%s?limit=1", hubBase, repoHandle), nil)
	req.Header.Set("x-api-key", apiKey)
	resp, err := httpCl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	var result struct {
		Commits []struct {
			CommitHash string `json:"commit_hash"`
		} `json:"commits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Commits) == 0 {
		return "", nil
	}
	return result.Commits[0].CommitHash, nil
}

// pullFromHub fetches the latest prompt from LangSmith Hub.
// Returns (system, humanTemplate, error).
func pullFromHub(repoHandle string) (string, string, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/commits/-/%s?limit=1", hubBase, repoHandle), nil)
	req.Header.Set("x-api-key", apiKey)
	resp, err := httpCl.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("pull: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("pull %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Commits []struct {
			Manifest map[string]interface{} `json:"manifest"`
		} `json:"commits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decode pull: %w", err)
	}

	if len(result.Commits) == 0 {
		return "", "", fmt.Errorf("no commits found for %s", repoHandle)
	}

	manifest := result.Commits[0].Manifest
	sys := extractTemplateFromManifest(manifest, "SystemMessagePromptTemplate")
	human := extractTemplateFromManifest(manifest, "HumanMessagePromptTemplate")
	return sys, human, nil
}

// ─── LangChain manifest helpers ─────────────────────────

// buildChatPromptManifest creates a LangChain-compatible ChatPromptTemplate manifest
// with named input variables so prompts are readable in LangSmith UI.
func buildChatPromptManifest(systemPrompt string, humanTemplate string, inputVars []string) map[string]interface{} {
	// Determine which vars appear in the system vs human template
	var systemVars []string
	var humanVars []string
	for _, v := range inputVars {
		placeholder := "{" + v + "}"
		if strings.Contains(systemPrompt, placeholder) {
			systemVars = append(systemVars, v)
		}
		if strings.Contains(humanTemplate, placeholder) {
			humanVars = append(humanVars, v)
		}
	}

	// Ensure non-nil slices for JSON
	if systemVars == nil {
		systemVars = []string{}
	}
	if humanVars == nil {
		humanVars = []string{}
	}
	allVars := make([]string, 0, len(inputVars))
	allVars = append(allVars, inputVars...)

	return map[string]interface{}{
		"lc":   1,
		"type": "constructor",
		"id":   []string{"langchain", "prompts", "chat", "ChatPromptTemplate"},
		"kwargs": map[string]interface{}{
			"input_variables": allVars,
			"messages": []interface{}{
				map[string]interface{}{
					"lc":   1,
					"type": "constructor",
					"id":   []string{"langchain", "prompts", "chat", "SystemMessagePromptTemplate"},
					"kwargs": map[string]interface{}{
						"prompt": map[string]interface{}{
							"lc":   1,
							"type": "constructor",
							"id":   []string{"langchain", "prompts", "prompt", "PromptTemplate"},
							"kwargs": map[string]interface{}{
								"input_variables": systemVars,
								"template":        systemPrompt,
								"template_format": "f-string",
							},
						},
					},
				},
				map[string]interface{}{
					"lc":   1,
					"type": "constructor",
					"id":   []string{"langchain", "prompts", "chat", "HumanMessagePromptTemplate"},
					"kwargs": map[string]interface{}{
						"prompt": map[string]interface{}{
							"lc":   1,
							"type": "constructor",
							"id":   []string{"langchain", "prompts", "prompt", "PromptTemplate"},
							"kwargs": map[string]interface{}{
								"input_variables": humanVars,
								"template":        humanTemplate,
								"template_format": "f-string",
							},
						},
					},
				},
			},
		},
	}
}

// extractTemplateFromManifest navigates a LangChain ChatPromptTemplate manifest
// and extracts the template string for the given message type
// (e.g. "SystemMessagePromptTemplate" or "HumanMessagePromptTemplate").
func extractTemplateFromManifest(manifest map[string]interface{}, messageType string) string {
	kwargs, ok := manifest["kwargs"].(map[string]interface{})
	if !ok {
		return ""
	}

	messages, ok := kwargs["messages"].([]interface{})
	if !ok {
		return ""
	}

	for _, msg := range messages {
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}

		ids, ok := m["id"].([]interface{})
		if !ok || len(ids) < 4 {
			continue
		}
		if fmt.Sprint(ids[3]) != messageType {
			continue
		}

		msgKwargs, ok := m["kwargs"].(map[string]interface{})
		if !ok {
			continue
		}
		prompt, ok := msgKwargs["prompt"].(map[string]interface{})
		if !ok {
			continue
		}
		promptKwargs, ok := prompt["kwargs"].(map[string]interface{})
		if !ok {
			continue
		}
		if tpl, ok := promptKwargs["template"].(string); ok {
			return tpl
		}
	}

	return ""
}
