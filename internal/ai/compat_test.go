package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleAndOllamaRejectEmptyContent(t *testing.T) {
	for _, content := range []string{"", "  \n\t"} {
		oai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + quote(content) + `}}]}`))
		}))
		ol := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"message":{"content":` + quote(content) + `}}`))
		}))
		if got, err := NewOpenAI("k", "m", oai.URL).GenerateText(context.Background(), TextRequest{}); err == nil {
			t.Errorf("openai accepted %q -> %q", content, got)
		}
		if got, err := NewOllama(ol.URL, "m", "").GenerateText(context.Background(), TextRequest{}); err == nil {
			t.Errorf("ollama accepted %q -> %q", content, got)
		}
		oai.Close()
		ol.Close()
	}
}

func quote(s string) string {
	b, _ := jsonMarshalString(s)
	return b
}

func jsonMarshalString(s string) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}
