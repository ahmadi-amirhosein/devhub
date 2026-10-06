package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// parseSpec pulls the first JSON object out of a model reply (models sometimes
// wrap JSON in prose or code fences) and bounds every field.
func parseSpec(text string) (*Spec, error) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, errors.New("no JSON object in model output")
	}
	var s Spec
	if err := json.Unmarshal([]byte(text[start:end+1]), &s); err != nil {
		return nil, fmt.Errorf("model returned invalid JSON: %w", err)
	}
	return sanitize(&s), nil
}

// postJSON sends in as JSON and decodes the reply into out. API keys travel in
// headers only, so they never end up in URLs or error messages.
func postJSON(ctx context.Context, c *http.Client, url string, headers map[string]string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, b)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func wrapClientText(description string) string {
	return "<client_text>\n" + description + "\n</client_text>"
}

// ---- Google Gemini ----

type Gemini struct {
	APIKey string
	Model  string
	HTTP   *http.Client
}

func NewGemini(apiKey, model string) *Gemini {
	return &Gemini{APIKey: apiKey, Model: model, HTTP: &http.Client{Timeout: 90 * time.Second}}
}

func (g *Gemini) Analyze(ctx context.Context, description string) (*Spec, error) {
	in := map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": systemPrompt}}},
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]string{{"text": wrapClientText(description)}},
		}},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"maxOutputTokens":  2048, // headroom: thinking models count reasoning tokens here
			"temperature":      0.2,
		},
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	url := "https://generativelanguage.googleapis.com/v1beta/models/" + g.Model + ":generateContent"
	if err := postJSON(ctx, g.HTTP, url, map[string]string{"x-goog-api-key": g.APIKey}, in, &out); err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	if len(out.Candidates) == 0 {
		return nil, errors.New("gemini: empty or blocked response")
	}
	var sb strings.Builder
	for _, p := range out.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}
	return parseSpec(sb.String())
}

// ---- Anthropic Claude ----

type Claude struct {
	APIKey string
	Model  string
	HTTP   *http.Client
}

func NewClaude(apiKey, model string) *Claude {
	return &Claude{APIKey: apiKey, Model: model, HTTP: &http.Client{Timeout: 90 * time.Second}}
}

func (c *Claude) Analyze(ctx context.Context, description string) (*Spec, error) {
	in := map[string]any{
		"model":      c.Model,
		"max_tokens": 1024,
		"system":     systemPrompt,
		"messages":   []map[string]string{{"role": "user", "content": wrapClientText(description)}},
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	headers := map[string]string{"x-api-key": c.APIKey, "anthropic-version": "2023-06-01"}
	if err := postJSON(ctx, c.HTTP, "https://api.anthropic.com/v1/messages", headers, in, &out); err != nil {
		return nil, fmt.Errorf("claude: %w", err)
	}
	var sb strings.Builder
	for _, b := range out.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return parseSpec(sb.String())
}
