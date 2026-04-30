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

// OllamaProvider uses the Ollama /api/chat endpoint.
// Text and vision use the same endpoint; vision just adds an "images" array.
type OllamaProvider struct {
	baseURL     string
	model       string
	visionModel string // if empty, SupportsVision returns false
	client      *http.Client
}

func NewOllama(baseURL, model, visionModel string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaProvider{
		baseURL:     baseURL,
		model:       model,
		visionModel: visionModel,
		client:      &http.Client{},
	}
}

func (p *OllamaProvider) Name() string        { return "ollama" }
func (p *OllamaProvider) SupportsVision() bool { return p.visionModel != "" }

func (p *OllamaProvider) GenerateText(ctx context.Context, req TextRequest) (string, error) {
	if p.model == "" {
		return "", fmt.Errorf("ollama: model not configured")
	}
	msgs := []ollamaMessage{}
	if req.SystemPrompt != "" {
		msgs = append(msgs, ollamaMessage{Role: "system", Content: req.SystemPrompt})
	}
	msgs = append(msgs, ollamaMessage{Role: "user", Content: req.UserPrompt})
	return p.call(ctx, p.model, msgs)
}

func (p *OllamaProvider) GenerateWithVision(ctx context.Context, req VisionRequest) (string, error) {
	if p.visionModel == "" {
		return "", fmt.Errorf("ollama: vision_model not configured")
	}
	imgB64 := base64.StdEncoding.EncodeToString(req.ImageData)
	msgs := []ollamaMessage{}
	if req.SystemPrompt != "" {
		msgs = append(msgs, ollamaMessage{Role: "system", Content: req.SystemPrompt})
	}
	msgs = append(msgs, ollamaMessage{
		Role:    "user",
		Content: req.UserPrompt,
		Images:  []string{imgB64},
	})
	return p.call(ctx, p.visionModel, msgs)
}

// ─── internal types ───────────────────────────────────────────────────────────

type ollamaMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"` // base64-encoded, no data URL prefix
}

type ollamaReq struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaResp struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error,omitempty"`
}

func (p *OllamaProvider) call(ctx context.Context, model string, msgs []ollamaMessage) (string, error) {
	body, _ := json.Marshal(ollamaReq{Model: model, Messages: msgs, Stream: false})
	req, _ := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: http: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var or ollamaResp
	if err := json.Unmarshal(raw, &or); err != nil {
		return "", fmt.Errorf("ollama: decode: %w", err)
	}
	if or.Error != "" {
		return "", fmt.Errorf("ollama: %s", or.Error)
	}
	return or.Message.Content, nil
}
