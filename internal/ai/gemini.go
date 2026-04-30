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

type GeminiProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewGemini(apiKey, model, baseURL string) *GeminiProvider {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	if model == "" {
		model = "gemini-2.0-flash"
	}
	return &GeminiProvider{apiKey: apiKey, model: model, baseURL: baseURL, client: &http.Client{}}
}

func (p *GeminiProvider) Name() string        { return "gemini" }
func (p *GeminiProvider) SupportsVision() bool { return true }

func (p *GeminiProvider) GenerateText(ctx context.Context, req TextRequest) (string, error) {
	parts := []geminiPart{{Text: req.UserPrompt}}
	return p.call(ctx, req.SystemPrompt, parts, req.MaxTokens)
}

func (p *GeminiProvider) GenerateWithVision(ctx context.Context, req VisionRequest) (string, error) {
	parts := []geminiPart{
		{
			InlineData: &geminiInlineData{
				MimeType: req.MimeType,
				Data:     base64.StdEncoding.EncodeToString(req.ImageData),
			},
		},
		{Text: req.UserPrompt},
	}
	return p.call(ctx, req.SystemPrompt, parts, req.MaxTokens)
}

// ─── internal types ───────────────────────────────────────────────────────────

type geminiPart struct {
	Text       string           `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type geminiReq struct {
	SystemInstruction *geminiSystem      `json:"system_instruction,omitempty"`
	Contents          []geminiContent    `json:"contents"`
	GenerationConfig  geminiGenConfig    `json:"generation_config"`
}

type geminiSystem struct {
	Parts []geminiPart `json:"parts"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiResp struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *GeminiProvider) call(ctx context.Context, system string, parts []geminiPart, maxTokens int) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("gemini: api_key not configured")
	}
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	reqBody := geminiReq{
		Contents: []geminiContent{{Role: "user", Parts: parts}},
		GenerationConfig: geminiGenConfig{MaxOutputTokens: maxTokens},
	}
	if system != "" {
		reqBody.SystemInstruction = &geminiSystem{Parts: []geminiPart{{Text: system}}}
	}
	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.baseURL, p.model, p.apiKey)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini: http: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var gr geminiResp
	if err := json.Unmarshal(raw, &gr); err != nil {
		return "", fmt.Errorf("gemini: decode: %w", err)
	}
	if gr.Error != nil {
		return "", fmt.Errorf("gemini: %s", gr.Error.Message)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini: empty response (status %d)", resp.StatusCode)
	}
	return gr.Candidates[0].Content.Parts[0].Text, nil
}
