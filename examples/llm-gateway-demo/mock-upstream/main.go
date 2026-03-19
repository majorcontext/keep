// Mock Anthropic API server for the Keep LLM gateway demo.
//
// Returns canned responses:
// - If the user message contains "delete" or "remove", responds with a
//   tool_use block containing "rm -rf" (to demonstrate blocking).
// - Otherwise, responds with a simple text message.
//
// Also exposes /debug/last-request to inspect what the gateway forwarded.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
)

var (
	lastRequest     json.RawMessage
	lastRequestLock sync.Mutex
)

func main() {
	http.HandleFunc("/v1/messages", handleMessages)
	http.HandleFunc("/debug/last-request", handleDebug)

	log.Println("mock-anthropic listening on :18081")
	if err := http.ListenAndServe(":18081", nil); err != nil {
		log.Fatal(err)
	}
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}
	defer r.Body.Close()

	// Store for debug endpoint
	lastRequestLock.Lock()
	lastRequest = json.RawMessage(body)
	lastRequestLock.Unlock()

	// Parse to decide which canned response to return
	var req map[string]any
	json.Unmarshal(body, &req)

	// Check if any user message mentions delete/remove
	wantsDestructive := false
	if messages, ok := req["messages"].([]any); ok {
		for _, m := range messages {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if content, ok := msg["content"].(string); ok {
				lower := strings.ToLower(content)
				if strings.Contains(lower, "delete") || strings.Contains(lower, "remove") {
					wantsDestructive = true
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")

	if wantsDestructive {
		// Return a response with a destructive tool_use
		fmt.Fprint(w, `{
  "id": "msg_demo_destructive",
  "type": "message",
  "role": "assistant",
  "model": "claude-sonnet-4-20250514",
  "stop_reason": "tool_use",
  "content": [
    {"type": "text", "text": "I'll delete those old backup files for you."},
    {
      "type": "tool_use",
      "id": "toolu_demo_1",
      "name": "bash",
      "input": {"command": "rm -rf /data/old-backups"}
    }
  ],
  "usage": {"input_tokens": 50, "output_tokens": 30}
}`)
	} else {
		// Return a simple text response
		fmt.Fprint(w, `{
  "id": "msg_demo_normal",
  "type": "message",
  "role": "assistant",
  "model": "claude-sonnet-4-20250514",
  "stop_reason": "end_turn",
  "content": [
    {"type": "text", "text": "2 + 2 = 4."}
  ],
  "usage": {"input_tokens": 20, "output_tokens": 10}
}`)
	}
}

func handleDebug(w http.ResponseWriter, r *http.Request) {
	lastRequestLock.Lock()
	data := lastRequest
	lastRequestLock.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if data == nil {
		fmt.Fprint(w, "{}")
		return
	}
	w.Write(data)
}
