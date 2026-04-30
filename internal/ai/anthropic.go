package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type AnthropicProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewAnthropic(apiKey, model, baseURL string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	return &AnthropicProvider{apiKey: apiKey, model: model, baseURL: baseURL, client: &http.Client{}}
}

func (p *AnthropicProvider) Name() string        { return "anthropic" }
func (p *AnthropicProvider) SupportsVision() bool { return true }

func (p *AnthropicProvider) GenerateText(ctx context.Context, req TextRequest) (string, error) {
	return p.call(ctx, req.SystemPrompt, []anthropicContent{{Type: "text", Text: req.UserPrompt}}, req.MaxTokens)
}

func (p *AnthropicProvider) GenerateWithVision(ctx context.Context, req VisionRequest) (string, error) {
	content := []anthropicContent{
		{
			Type: "image",
			Source: &anthropicImageSource{
				Type:      "base64",
				MediaType: req.MimeType,
				Data:      base64.StdEncoding.EncodeToString(req.ImageData),
			},
		},
		{Type: "text", Text: req.UserPrompt},
	}
	return p.call(ctx, req.SystemPrompt, content, req.MaxTokens)
}

// ─── internal types ───────────────────────────────────────────────────────────

type anthropicContent struct {
	Type   string               `json:"type"`
	Text   string               `json:"text,omitempty"`
	Source *anthropicImageSource `json:"source,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicReq struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicResp struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *AnthropicProvider) call(ctx context.Context, system string, content []anthropicContent, maxTokens int) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("anthropic: api_key not configured")
	}
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	body, _ := json.Marshal(anthropicReq{
		Model:     p.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  []anthropicMessage{{Role: "user", Content: content}},
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: http: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var ar anthropicResp
	if err := json.Unmarshal(raw, &ar); err != nil {
		return "", fmt.Errorf("anthropic: decode: %w", err)
	}
	if ar.Error != nil {
		return "", fmt.Errorf("anthropic: %s", ar.Error.Message)
	}
	if len(ar.Content) == 0 {
		return "", fmt.Errorf("anthropic: empty response (status %d)", resp.StatusCode)
	}
	return ar.Content[0].Text, nil
}
