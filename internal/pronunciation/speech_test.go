package pronunciation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"mathtrainer/internal/googletts"
)

func fakeSpeechService(t *testing.T, handler http.HandlerFunc) (*Service, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dir := t.TempDir()
	s := New(nil)
	s.cacheDir = dir
	s.tts = &googletts.Client{
		Endpoint: server.URL,
		Token:    func(context.Context) (string, string, error) { return "fake-token", "", nil },
	}
	return s, dir
}

func TestSynthesizeBritishSpeechUsesGoogleEnGB(t *testing.T) {
	var requests int32
	var got struct {
		Input struct {
			Text string `json:"text"`
		} `json:"input"`
		Voice struct {
			LanguageCode string `json:"languageCode"`
			Name         string `json:"name"`
		} `json:"voice"`
		AudioConfig struct {
			AudioEncoding string `json:"audioEncoding"`
		} `json:"audioConfig"`
	}
	s, dir := fakeSpeechService(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]string{"audioContent": base64.StdEncoding.EncodeToString([]byte("AUDIO-BYTES"))})
	})

	url, cached, err := s.SynthesizeBritishSpeech(context.Background(), "colour")
	if err != nil {
		t.Fatal(err)
	}
	if cached {
		t.Fatal("first request must not be cached")
	}
	if requests != 1 || got.Voice.LanguageCode != "en-GB" || got.Input.Text != "colour" ||
		got.Voice.Name != "en-GB-Chirp3-HD-Aoede" || got.AudioConfig.AudioEncoding != "MP3" {
		t.Fatalf("requests=%d body=%+v", requests, got)
	}
	if !strings.HasPrefix(url, "/audio/tts/english/") {
		t.Fatalf("url = %q", url)
	}
	if !strings.HasSuffix(url, ".mp3") {
		t.Fatalf("url = %q", url)
	}
	data, err := os.ReadFile(filepath.Join(dir, strings.TrimPrefix(url, "/audio/tts/english/")))
	if err != nil || string(data) != "AUDIO-BYTES" {
		t.Fatalf("file = %q, err = %v", data, err)
	}

	url2, cached2, err := s.SynthesizeBritishSpeech(context.Background(), "colour")
	if err != nil {
		t.Fatal(err)
	}
	if !cached2 || url2 != url || requests != 1 {
		t.Fatalf("cached=%v url=%q requests=%d", cached2, url2, requests)
	}
}

func TestSynthesizeBritishSpeechServerErrorLeavesNoFile(t *testing.T) {
	s, dir := fakeSpeechService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	})
	if _, _, err := s.SynthesizeBritishSpeech(context.Background(), "colour"); err == nil {
		t.Fatal("expected an error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("cache directory holds %d files", len(entries))
	}
}
