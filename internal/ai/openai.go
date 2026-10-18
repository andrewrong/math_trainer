package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIProvider handles api.openai.com and any OpenAI-compatible endpoint
// (LM Studio, OpenRouter, etc.) by varying baseURL and extraHeaders.
type OpenAIProvider struct {
	name         string
	apiKey       string
	model        string
	baseURL      string
	extraHeaders map[string]string
	client       *http.Client
}

func NewOpenAI(apiKey, model, baseURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &OpenAIProvider{name: "openai", apiKey: apiKey, model: model, baseURL: baseURL, client: &http.Client{}}
}

func NewLMStudio(apiKey, model, baseURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "http://localhost:1234/v1"
	}
	if apiKey == "" {
		apiKey = "lm-studio"
	}
	return &OpenAIProvider{name: "lmstudio", apiKey: apiKey, model: model, baseURL: baseURL, client: &http.Client{}}
}

// NewOpenRouter creates a provider that routes through openrouter.ai.
// model examples: "anthropic/claude-3-5-haiku", "google/gemini-2.0-flash",
// "openai/gpt-4o-mini", "meta-llama/llama-3.1-8b-instruct:free"
func NewOpenRouter(apiKey, model string) *OpenAIProvider {
	if model == "" {
		model = "anthropic/claude-3-5-haiku"
	}
	return &OpenAIProvider{
		name:    "openrouter",
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://openrouter.ai/api/v1",
		extraHeaders: map[string]string{
			"HTTP-Referer": "http://localhost:8080",
			"X-Title":      "Math Trainer",
		},
		client: &http.Client{},
	}
}

func (p *OpenAIProvider) Name() string        { return p.name }
func (p *OpenAIProvider) SupportsVision() bool { return true }

func (p *OpenAIProvider) GenerateText(ctx context.Context, req TextRequest) (string, error) {
	msgs := []oaiMessage{}
	if req.SystemPrompt != "" {
		msgs = append(msgs, oaiMessage{Role: "system", Content: req.SystemPrompt})
	}
	msgs = append(msgs, oaiMessage{Role: "user", Content: req.UserPrompt})
	return p.call(ctx, msgs, req.MaxTokens)
}

func (p *OpenAIProvider) GenerateWithVision(ctx context.Context, req VisionRequest) (string, error) {
	dataURL := fmt.Sprintf("data:%s;base64,%s", req.MimeType, base64.StdEncoding.EncodeToString(req.ImageData))
	userContent := []oaiContentPart{
		{Type: "image_url", ImageURL: &oaiImageURL{URL: dataURL}},
		{Type: "text", Text: req.UserPrompt},
	}
	msgs := []oaiMessage{}
	if req.SystemPrompt != "" {
		msgs = append(msgs, oaiMessage{Role: "system", Content: req.SystemPrompt})
	}
	msgs = append(msgs, oaiMessage{Role: "user", ContentParts: userContent})
	return p.call(ctx, msgs, req.MaxTokens)
}

// ─── internal types ───────────────────────────────────────────────────────────

type oaiContentPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *oaiImageURL `json:"image_url,omitempty"`
}

type oaiImageURL struct {
	URL string `json:"url"`
}

// oaiMessage supports both string content and multi-part content.
type oaiMessage struct {
	Role         string           `json:"role"`
	Content      string           `json:"content,omitempty"`
	ContentParts []oaiContentPart `json:"-"`
}

func (m oaiMessage) MarshalJSON() ([]byte, error) {
	if len(m.ContentParts) > 0 {
		return json.Marshal(struct {
			Role    string           `json:"role"`
			Content []oaiContentPart `json:"content"`
		}{m.Role, m.ContentParts})
	}
	return json.Marshal(struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{m.Role, m.Content})
}

type oaiReq struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens,omitempty"`
	Messages  []oaiMessage `json:"messages"`
}

type oaiResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *OpenAIProvider) call(ctx context.Context, msgs []oaiMessage, maxTokens int) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("%s: api_key not configured", p.name)
	}
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	body, _ := json.Marshal(oaiReq{Model: p.model, MaxTokens: maxTokens, Messages: msgs})
	req, _ := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range p.extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: http: %w", p.name, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var or oaiResp
	if err := json.Unmarshal(raw, &or); err != nil {
		return "", fmt.Errorf("%s: decode: %w", p.name, err)
	}
	if or.Error != nil {
		return "", fmt.Errorf("%s: %s", p.name, or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return "", fmt.Errorf("%s: empty response (status %d)", p.name, resp.StatusCode)
	}
	if strings.TrimSpace(or.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("%s: empty content (status %d)", p.name, resp.StatusCode)
	}
	return or.Choices[0].Message.Content, nil
}
