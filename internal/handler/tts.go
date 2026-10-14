package handler

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/googletts"
	"mathtrainer/internal/model"
)

type TTSHandler struct {
	client   *googletts.Client
	cacheDir string
}

func NewTTS() *TTSHandler {
	return &TTSHandler{client: googletts.Default(), cacheDir: filepath.Join("web", "audio", "tts", "chinese")}
}

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
	path := filepath.Join(h.cacheDir, name)
	if _, err := os.Stat(path); err == nil {
		c.JSON(http.StatusOK, gin.H{"url": rel, "cached": true})
		return
	}
	audio, err := h.googleChineseTTS(c, text)
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

func (h *TTSHandler) googleChineseTTS(ctx context.Context, text string) ([]byte, error) {
	return h.client.Synthesize(ctx, text, googletts.Voice{LanguageCode: "cmn-CN", Name: "cmn-CN-Chirp3-HD-Aoede"})
}
