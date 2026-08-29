// Command mock-openai-backend is a tiny stand-in inference server for
// trying inferoute (and its dashboard) without real GPUs. It answers the
// handful of OpenAI-compatible routes inferoute and common SDKs touch:
//
//	GET  /                       health check (inferoute pings this)
//	GET  /healthz                health check
//	GET  /v1/models              lists the one model this instance serves
//	POST /v1/chat/completions    echoes the prompt back, streaming or not
//
// Run two on different ports, point a backend at each in config.json, and
// you have something to route between:
//
//	go run ./scripts/mock-openai-backend -addr :11434 -model llama3 -name alpha
//	go run ./scripts/mock-openai-backend -addr :11435 -model llama3 -name bravo
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", ":11434", "address to listen on")
	model := flag.String("model", "llama3", "model id this instance serves")
	name := flag.String("name", "mock", "name echoed in replies so you can tell instances apart")
	delay := flag.Duration("delay", 250*time.Millisecond, "artificial latency per request")
	flag.Parse()

	mux := http.NewServeMux()

	health := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mux.HandleFunc("GET /", health)
	mux.HandleFunc("GET /healthz", health)

	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": *model, "object": "model", "owned_by": *name}},
		})
	})

	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		time.Sleep(*delay)

		var prompt string
		if len(req.Messages) > 0 {
			prompt = req.Messages[len(req.Messages)-1].Content
		}
		reply := fmt.Sprintf("[%s] you said: %s", *name, prompt)
		created := time.Now().Unix()
		id := fmt.Sprintf("chatcmpl-mock-%d", created)

		if !req.Stream {
			writeJSON(w, map[string]any{
				"id": id, "object": "chat.completion", "created": created, "model": req.Model,
				"choices": []map[string]any{{
					"index":         0,
					"message":       map[string]string{"role": "assistant", "content": reply},
					"finish_reason": "stop",
				}},
				"usage": map[string]int{"prompt_tokens": len(prompt), "completion_tokens": len(reply), "total_tokens": len(prompt) + len(reply)},
			})
			return
		}

		// Stream the reply one word per SSE chunk, OpenAI delta shape.
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		for i, word := range strings.Fields(reply) {
			if i > 0 {
				word = " " + word
			}
			chunk := map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model,
				"choices": []map[string]any{{"index": 0, "delta": map[string]string{"content": word}}},
			}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
			time.Sleep(40 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	})

	log.Printf("mock-openai-backend %q serving model %q on %s", *name, *model, *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
