package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/googletts"
)

func TestChineseSpeechUsesChineseVoiceAndCaches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requests := 0
	var got struct {
		Input struct {
			Text string `json:"text"`
		} `json:"input"`
		Voice struct {
			LanguageCode string `json:"languageCode"`
			Name         string `json:"name"`
		} `json:"voice"`
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]string{"audioContent": base64.StdEncoding.EncodeToString([]byte("ZH-AUDIO"))})
	}))
	defer fake.Close()
	dir := t.TempDir()
	h := &TTSHandler{
		client: &googletts.Client{
			Endpoint: fake.URL,
			Token:    func(context.Context) (string, string, error) { return "fake-token", "", nil },
		},
		cacheDir: dir,
	}
	r := gin.New()
	h.Register(r)

	post := func() (int, map[string]any) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/tts/chinese", strings.NewReader(`{"text":"你好"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}

	code, body := post()
	if code != http.StatusOK || body["cached"] != false {
		t.Fatalf("code=%d body=%v", code, body)
	}
	if requests != 1 || got.Input.Text != "你好" || got.Voice.LanguageCode != "cmn-CN" || got.Voice.Name != "cmn-CN-Chirp3-HD-Aoede" {
		t.Fatalf("requests=%d body=%+v", requests, got)
	}
	url, _ := body["url"].(string)
	if !strings.HasPrefix(url, "/audio/tts/chinese/") {
		t.Fatalf("url = %q", url)
	}
	data, err := os.ReadFile(filepath.Join(dir, strings.TrimPrefix(url, "/audio/tts/chinese/")))
	if err != nil || string(data) != "ZH-AUDIO" {
		t.Fatalf("file = %q, err = %v", data, err)
	}
	code, body = post()
	if code != http.StatusOK || body["cached"] != true || body["url"] != url || requests != 1 {
		t.Fatalf("code=%d body=%v requests=%d", code, body, requests)
	}
}
