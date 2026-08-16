package tracing

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// ─── Config ──────────────────────────────────────────────

var (
	apiKey  string
	project string
	enabled bool
	client  = &http.Client{Timeout: 10 * time.Second}
)

const langsmithURL = "https://api.smith.langchain.com"

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func Init() {
	apiKey = os.Getenv("LANGSMITH_API_KEY")
	project = os.Getenv("LANGSMITH_PROJECT")
	if project == "" {
		project = "pine-journal"
	}
	enabled = apiKey != ""
	if enabled {
		log.Printf("[Tracing] LangSmith enabled (project: %s, key: %s...)\n", project, apiKey[:8])
	} else {
		log.Println("[Tracing] LangSmith disabled — set LANGSMITH_API_KEY to enable")
	}
}

func Enabled() bool {
	return enabled
}

// ─── Trace span ──────────────────────────────────────────

type Span struct {
	RunID     string
	ParentID  string
	Name      string
	RunType   string
	StartTime time.Time
}

// StartLLMSpan begins tracing an LLM call with full prompt visibility.
// Sends messages in the standard chat format so LangSmith renders them properly.
func StartLLMSpan(name string, model string, system string, userInput string, parentID string) *Span {
	if !enabled {
		return nil
	}

	span := &Span{
		RunID:     newUUID(),
		ParentID:  parentID,
		Name:      name,
		RunType:   "llm",
		StartTime: time.Now().UTC(),
	}

	// Build messages array for LangSmith's chat model view
	messages := []map[string]interface{}{}
	if system != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": system,
		})
	}
	messages = append(messages, map[string]interface{}{
		"role":    "user",
		"content": userInput,
	})

	payload := map[string]interface{}{
		"id":           span.RunID,
		"name":         name,
		"run_type":     "llm",
		"start_time":   span.StartTime.Format(time.RFC3339Nano),
		"session_name": project,
		"inputs": map[string]interface{}{
			"messages": messages,
		},
		"extra": map[string]interface{}{
			"metadata": map[string]interface{}{
				"model":    model,
				"provider": "amazon_bedrock",
			},
			"invocation_params": map[string]interface{}{
				"model":    model,
				"provider": "amazon_bedrock",
			},
		},
		"serialized": map[string]interface{}{
			"name": "ChatBedrock",
			"kwargs": map[string]interface{}{
				"model_id": model,
			},
		},
	}

	if parentID != "" {
		payload["parent_run_id"] = parentID
	}

	go postRun(payload)
	return span
}

// StartChatSpan traces a multi-turn chat with full conversation history.
func StartChatSpan(name string, model string, system string, chatMessages []map[string]string, parentID string) *Span {
	if !enabled {
		return nil
	}

	span := &Span{
		RunID:     newUUID(),
		ParentID:  parentID,
		Name:      name,
		RunType:   "llm",
		StartTime: time.Now().UTC(),
	}

	// Build full messages array with system + conversation history
	messages := []map[string]interface{}{}
	if system != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": system,
		})
	}
	for _, msg := range chatMessages {
		messages = append(messages, map[string]interface{}{
			"role":    msg["role"],
			"content": msg["content"],
		})
	}

	payload := map[string]interface{}{
		"id":           span.RunID,
		"name":         name,
		"run_type":     "llm",
		"start_time":   span.StartTime.Format(time.RFC3339Nano),
		"session_name": project,
		"inputs": map[string]interface{}{
			"messages": messages,
		},
		"extra": map[string]interface{}{
			"metadata": map[string]interface{}{
				"model":         model,
				"provider":      "amazon_bedrock",
				"message_count": len(chatMessages),
			},
			"invocation_params": map[string]interface{}{
				"model":    model,
				"provider": "amazon_bedrock",
			},
		},
		"serialized": map[string]interface{}{
			"name": "ChatBedrock",
			"kwargs": map[string]interface{}{
				"model_id": model,
			},
		},
	}

	if parentID != "" {
		payload["parent_run_id"] = parentID
	}

	go postRun(payload)
	return span
}

// StartChainSpan begins tracing a handler-level chain.
func StartChainSpan(name string) *Span {
	if !enabled {
		return nil
	}

	span := &Span{
		RunID:     newUUID(),
		Name:      name,
		RunType:   "chain",
		StartTime: time.Now().UTC(),
	}

	payload := map[string]interface{}{
		"id":           span.RunID,
		"name":         name,
		"run_type":     "chain",
		"start_time":   span.StartTime.Format(time.RFC3339Nano),
		"session_name": project,
		"inputs":       map[string]interface{}{},
	}

	go postRun(payload)
	return span
}

// EndSpan completes a trace span with the full assistant response.
func EndSpan(span *Span, output string, err error) {
	if span == nil || !enabled {
		return
	}

	endTime := time.Now().UTC()

	payload := map[string]interface{}{
		"end_time": endTime.Format(time.RFC3339Nano),
	}

	if err != nil {
		payload["error"] = err.Error()
		payload["outputs"] = map[string]interface{}{
			"error": err.Error(),
		}
	} else {
		// Send as a proper chat completion so LangSmith shows it in message view
		payload["outputs"] = map[string]interface{}{
			"generations": []map[string]interface{}{
				{
					"text": output,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": output,
					},
				},
			},
		}
	}

	go patchRun(span.RunID, payload)
}

// EndChainSpan completes a chain span with structured inputs/outputs.
func EndChainSpan(span *Span, inputs map[string]interface{}, output string, err error) {
	if span == nil || !enabled {
		return
	}

	endTime := time.Now().UTC()

	payload := map[string]interface{}{
		"end_time": endTime.Format(time.RFC3339Nano),
		"inputs":   inputs,
	}

	if err != nil {
		payload["error"] = err.Error()
		payload["outputs"] = map[string]interface{}{"error": err.Error()}
	} else {
		payload["outputs"] = map[string]interface{}{"output": output}
	}

	go patchRun(span.RunID, payload)
}

// ─── HTTP helpers ────────────────────────────────────────

func postRun(payload map[string]interface{}) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[Tracing] marshal error: %v\n", err)
		return
	}

	req, err := http.NewRequest("POST", langsmithURL+"/runs", bytes.NewReader(body))
	if err != nil {
		log.Printf("[Tracing] request error: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Tracing] POST /runs error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		log.Printf("[Tracing] POST /runs returned %d: %s\n", resp.StatusCode, string(respBody))
	}
}

func patchRun(runID string, payload map[string]interface{}) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[Tracing] marshal error: %v\n", err)
		return
	}

	url := fmt.Sprintf("%s/runs/%s", langsmithURL, runID)
	req, err := http.NewRequest("PATCH", url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[Tracing] request error: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Tracing] PATCH /runs error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		log.Printf("[Tracing] PATCH /runs/%s returned %d: %s\n", runID, resp.StatusCode, string(respBody))
	}
}
