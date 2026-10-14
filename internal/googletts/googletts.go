// Package googletts synthesizes speech with Google Text-to-Speech. Chinese and
// English speech share it, and with it the one way of obtaining an access token.
package googletts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultEndpoint = "https://texttospeech.googleapis.com/v1/text:synthesize"

// Voice selects the language and, optionally, a named voice.
type Voice struct {
	LanguageCode string `json:"languageCode"`
	Name         string `json:"name,omitempty"`
}

// Client calls the Google endpoint. Endpoint and Token can be replaced in a test.
type Client struct {
	Endpoint string
	Token    func(ctx context.Context) (token, quotaProject string, err error)
}

// Default returns a client for the real endpoint and the shared token source.
func Default() *Client {
	return &Client{Endpoint: defaultEndpoint, Token: adcToken}
}

// Synthesize returns MP3 audio for text.
func (c *Client) Synthesize(ctx context.Context, text string, voice Voice) ([]byte, error) {
	token, quotaProject, err := c.Token(ctx)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{
		"input":       map[string]string{"text": text},
		"voice":       voice,
		"audioConfig": map[string]string{"audioEncoding": "MP3"},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if quotaProject != "" {
		req.Header.Set("X-Goog-User-Project", quotaProject)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		AudioContent string `json:"audioContent"`
		Error        struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeErr := json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 {
		if out.Error.Message != "" {
			return nil, fmt.Errorf("google tts: %s", out.Error.Message)
		}
		return nil, fmt.Errorf("google tts: http %d", resp.StatusCode)
	}
	if decodeErr != nil {
		return nil, decodeErr
	}
	audio, err := base64.StdEncoding.DecodeString(out.AudioContent)
	if err != nil {
		return nil, err
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("google tts: empty audio")
	}
	return audio, nil
}

func adcToken(ctx context.Context) (string, string, error) {
	if token, quotaProject, err := adcFileToken(ctx, ".secrets/google_adc.json"); err == nil {
		return token, quotaProject, nil
	}
	gcloud, err := exec.LookPath("gcloud")
	if err != nil {
		gcloud = "/opt/homebrew/bin/gcloud"
	}
	b, err := exec.CommandContext(ctx, gcloud, "auth", "application-default", "print-access-token", "--quiet").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("google adc token failed: %s", strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), "", nil
}

func adcFileToken(ctx context.Context, path string) (string, string, error) {
	var cred struct {
		ClientID       string `json:"client_id"`
		ClientSecret   string `json:"client_secret"`
		RefreshToken   string `json:"refresh_token"`
		QuotaProjectID string `json:"quota_project_id"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	if err := json.Unmarshal(b, &cred); err != nil {
		return "", "", err
	}
	form := url.Values{
		"client_id":     {cred.ClientID},
		"client_secret": {cred.ClientSecret},
		"refresh_token": {cred.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", err
	}
	if resp.StatusCode >= 300 || out.AccessToken == "" {
		return "", "", fmt.Errorf("google token refresh failed: %s %s", out.Error, out.Description)
	}
	return out.AccessToken, cred.QuotaProjectID, nil
}
