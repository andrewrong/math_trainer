package handler

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/model"
)

type TTSHandler struct{}

func NewTTS() *TTSHandler { return &TTSHandler{} }

func (h *TTSHandler) Register(r gin.IRouter) {
	r.POST("/tts/chinese", h.chinese)
}

type ttsReq struct {
	Text string `json:"text" binding:"required"`
}

func (h *TTSHandler) chinese(c *gin.Context) {
	var req ttsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" || utf8.RuneCountInString(text) > 200 {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "text must be 1-200 characters"})
		return
	}
	name := hashText(text) + ".mp3"
	rel := "/audio/tts/chinese/" + name
	path := filepath.Join("web", "audio", "tts", "chinese", name)
	if _, err := os.Stat(path); err == nil {
		c.JSON(http.StatusOK, gin.H{"url": rel, "cached": true})
		return
	}
	audio, err := googleChineseTTS(c, text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	if err := os.WriteFile(path, audio, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": rel, "cached": false})
}

func hashText(text string) string {
	sum := sha1.Sum([]byte("zh:" + text))
	return hex.EncodeToString(sum[:])
}

func googleChineseTTS(ctx context.Context, text string) ([]byte, error) {
	token, quotaProject, err := adcToken(ctx)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{
		"input": map[string]string{"text": text},
		"voice": map[string]string{
			"languageCode": "cmn-CN",
			"name":         "cmn-CN-Chirp3-HD-Aoede",
		},
		"audioConfig": map[string]string{"audioEncoding": "MP3"},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", "https://texttospeech.googleapis.com/v1/text:synthesize", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if quotaProject != "" {
		req.Header.Set("X-Goog-User-Project", quotaProject)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
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
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		if out.Error.Message != "" {
			return nil, fmt.Errorf("google tts: %s", out.Error.Message)
		}
		return nil, fmt.Errorf("google tts: http %d", resp.StatusCode)
	}
	return base64.StdEncoding.DecodeString(out.AudioContent)
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
