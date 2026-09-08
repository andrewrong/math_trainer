package ai

import (
	"context"
	"errors"
	"fmt"
	"log"
)

// TextRequest is used for plain-text generation (word cards, story, grammar, etc.).
type TextRequest struct {
	SystemPrompt     string
	UserPrompt       string
	MaxTokens        int
	ResponseMIMEType string
}

// VisionRequest is used when an image is part of the input (e.g. word list photo).
type VisionRequest struct {
	SystemPrompt     string
	UserPrompt       string
	ImageData        []byte
	MimeType         string // "image/jpeg" | "image/png" | "image/webp"
	MaxTokens        int
	ResponseMIMEType string
}

// Provider is implemented by every AI backend.
type Provider interface {
	Name() string
	GenerateText(ctx context.Context, req TextRequest) (string, error)
	GenerateWithVision(ctx context.Context, req VisionRequest) (string, error)
	// SupportsVision returns false for text-only models so the chain can skip them.
	SupportsVision() bool
}

// Chain tries providers in order and falls back on error.
// This is the only struct callers outside this package use.
type Chain struct {
	providers []Provider
}

type TextResult struct {
	Text     string
	Provider string
}

func NewChain(providers []Provider) *Chain {
	return &Chain{providers: providers}
}

// Available returns the names of all configured providers.
func (c *Chain) Available() []string {
	names := make([]string, len(c.providers))
	for i, p := range c.providers {
		names[i] = p.Name()
	}
	return names
}

func (c *Chain) GenerateText(ctx context.Context, req TextRequest) (string, error) {
	result, err := c.GenerateTextDetailed(ctx, req)
	return result.Text, err
}

// GenerateTextDetailed is used by evidence-backed features that need to retain
// which configured provider produced a result. Existing callers can continue to
// use GenerateText unchanged.
func (c *Chain) GenerateTextDetailed(ctx context.Context, req TextRequest) (TextResult, error) {
	if len(c.providers) == 0 {
		return TextResult{}, errors.New("no AI providers configured")
	}
	var errs []error
	for _, p := range c.providers {
		result, err := p.GenerateText(ctx, req)
		if err == nil {
			return TextResult{Text: result, Provider: p.Name()}, nil
		}
		log.Printf("[ai] text provider %s failed: %v", p.Name(), err)
		errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
	}
	return TextResult{}, fmt.Errorf("all providers failed: %w", errors.Join(errs...))
}

func (c *Chain) GenerateWithVision(ctx context.Context, req VisionRequest) (string, error) {
	if len(c.providers) == 0 {
		return "", errors.New("no AI providers configured")
	}
	var errs []error
	for _, p := range c.providers {
		if !p.SupportsVision() {
			log.Printf("[ai] vision provider %s skipped: vision not supported", p.Name())
			continue
		}
		result, err := p.GenerateWithVision(ctx, req)
		if err == nil {
			return result, nil
		}
		log.Printf("[ai] vision provider %s failed: %v", p.Name(), err)
		errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
	}
	if len(errs) == 0 {
		return "", errors.New("no vision-capable providers configured")
	}
	return "", fmt.Errorf("all vision providers failed: %w", errors.Join(errs...))
}
