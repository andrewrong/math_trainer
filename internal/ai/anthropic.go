package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
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
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// anthropicBadStopReasons end a reply that cannot be used as a complete answer.
var anthropicBadStopReasons = map[string]bool{
	"max_tokens":                    true,
	"model_context_window_exceeded": true,
	"pause_turn":                    true,
	"refusal":                       true,
	"tool_use":                      true,
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

	started := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: http: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var ar anthropicResp
	decodeErr := json.Unmarshal(raw, &ar)
	var text strings.Builder
	types := make([]string, 0, len(ar.Content))
	for _, blk := range ar.Content {
		types = append(types, blk.Type)
		if blk.Type == "text" {
			text.WriteString(blk.Text)
		}
	}
	blockTypes := strings.Join(types, ",")
	usage := ""
	if ar.Usage != nil {
		usage = fmt.Sprintf(" input_tokens=%d output_tokens=%d", ar.Usage.InputTokens, ar.Usage.OutputTokens)
	}
	log.Printf("[ai] anthropic call: model=%s status=%d stop_reason=%s blocks=%q text_len=%d duration=%s%s",
		p.model, resp.StatusCode, ar.StopReason, blockTypes, text.Len(), time.Since(started).Round(time.Millisecond), usage)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := ""
		if ar.Error != nil {
			msg = ": " + ar.Error.Message
		}
		return "", fmt.Errorf("anthropic: http status %d%s", resp.StatusCode, msg)
	}
	if decodeErr != nil {
		return "", fmt.Errorf("anthropic: decode: %w", decodeErr)
	}
	if ar.Error != nil {
		return "", fmt.Errorf("anthropic: %s", ar.Error.Message)
	}
	if anthropicBadStopReasons[ar.StopReason] {
		return "", fmt.Errorf("anthropic: unusable reply (status %d, stop_reason %s, blocks %q)", resp.StatusCode, ar.StopReason, blockTypes)
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", fmt.Errorf("anthropic: no text in reply (status %d, stop_reason %s, blocks %q)", resp.StatusCode, ar.StopReason, blockTypes)
	}
	return text.String(), nil
}
