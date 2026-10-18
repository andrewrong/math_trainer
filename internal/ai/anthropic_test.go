package ai

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func anthropicServer(t *testing.T, status int, body string) *AnthropicProvider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewAnthropic("k", "test-model", srv.URL)
}

func TestAnthropicIgnoresThinkingBlockAndReturnsText(t *testing.T) {
	p := anthropicServer(t, 200, `{"content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"hello"}],"stop_reason":"end_turn"}`)
	got, err := p.GenerateText(context.Background(), TextRequest{UserPrompt: "x"})
	if err != nil || got != "hello" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestAnthropicJoinsTextBlocksInOrder(t *testing.T) {
	p := anthropicServer(t, 200, `{"content":[{"type":"text","text":"ab "},{"type":"server_tool_use"},{"type":"text","text":" cd"}],"stop_reason":"end_turn"}`)
	got, err := p.GenerateText(context.Background(), TextRequest{UserPrompt: "x"})
	if err != nil || got != "ab  cd" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestAnthropicUnusableRepliesAreErrors(t *testing.T) {
	cases := []struct {
		name, body, want string
		status           int
	}{
		{"only thinking", `{"content":[{"type":"thinking","thinking":"SECRETTEXT"}],"stop_reason":"end_turn"}`, "stop_reason end_turn", 200},
		{"whitespace", `{"content":[{"type":"text","text":" \n SECRETTEXT"}],"stop_reason":"end_turn"}`, "stop_reason end_turn", 200},
		{"max_tokens", `{"content":[{"type":"text","text":"SECRETTEXT"}],"stop_reason":"max_tokens"}`, "max_tokens", 200},
		{"context window", `{"content":[{"type":"text","text":"SECRETTEXT"}],"stop_reason":"model_context_window_exceeded"}`, "model_context_window_exceeded", 200},
		{"pause_turn", `{"content":[{"type":"text","text":"SECRETTEXT"}],"stop_reason":"pause_turn"}`, "pause_turn", 200},
		{"refusal", `{"content":[{"type":"text","text":"SECRETTEXT"}],"stop_reason":"refusal"}`, "refusal", 200},
		{"tool_use", `{"content":[{"type":"text","text":"SECRETTEXT"}],"stop_reason":"tool_use"}`, "tool_use", 200},
		{"http 500", `{"content":[{"type":"text","text":"SECRETTEXT"}],"stop_reason":"end_turn"}`, "500", 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "whitespace" {
				c.body = strings.Replace(c.body, "SECRETTEXT", "", 1)
			}
			p := anthropicServer(t, c.status, c.body)
			got, err := p.GenerateText(context.Background(), TextRequest{UserPrompt: "x"})
			if err == nil {
				t.Fatalf("no error, got %q", got)
			}
			if !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "status "+strconv.Itoa(c.status)) || strings.Contains(err.Error(), "SECRETTEXT") {
				t.Fatalf("error %q", err)
			}
		})
	}
}

func TestAnthropicEndTurnTextIsReturned(t *testing.T) {
	p := anthropicServer(t, 200, `{"content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`)
	got, err := p.GenerateText(context.Background(), TextRequest{UserPrompt: "x"})
	if err != nil || got != "done" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestAnthropicLogsOneLinePerCallWithoutText(t *testing.T) {
	p := anthropicServer(t, 200, `{"content":[{"type":"thinking"},{"type":"text","text":"SECRETTEXT"}],"stop_reason":"end_turn","usage":{"input_tokens":7,"output_tokens":9}}`)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	if _, err := p.GenerateText(context.Background(), TextRequest{UserPrompt: "x"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("want one line: %q", out)
	}
	for _, w := range []string{"test-model", "status=200", "stop_reason=end_turn", "thinking,text", "text_len=10"} {
		if !strings.Contains(out, w) {
			t.Fatalf("log %q lacks %q", out, w)
		}
	}
	if strings.Contains(out, "SECRETTEXT") {
		t.Fatalf("log holds text: %q", out)
	}
}

func TestAnthropicHTTPErrorDoesNotExposeText(t *testing.T) {
	p := anthropicServer(t, 500,
		`{"error":{"message":"SECRETTEXT"},"content":[{"type":"text","text":"SECRETTEXT"}],"stop_reason":"end_turn"}`)

	_, err := p.GenerateText(context.Background(), TextRequest{})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected HTTP 500 error, got %v", err)
	}
	if strings.Contains(err.Error(), "SECRETTEXT") {
		t.Fatalf("response text exposed: %v", err)
	}
}

func TestAnthropicHTTPErrorFailsBeforeTheBodyArrives(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.(http.Flusher).Flush()
		<-release
	}))
	defer srv.Close()
	defer close(release)
	done := make(chan error, 1)
	go func() {
		_, err := NewAnthropic("k", "m", srv.URL).GenerateText(context.Background(), TextRequest{})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call waited for the body of an HTTP 500")
	}
}

func TestAnthropicTransportFailureIsLogged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	if _, err := NewAnthropic("k", "test-model", url).GenerateText(context.Background(), TextRequest{}); err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(buf.String(), "anthropic call") || !strings.Contains(buf.String(), "test-model") {
		t.Fatalf("log %q", buf.String())
	}
}
