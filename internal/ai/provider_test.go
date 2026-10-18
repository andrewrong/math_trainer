package ai

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeProvider struct {
	name  string
	reply string
	err   error
	calls int
	block bool
	hook  func()
}

func (f *fakeProvider) Name() string         { return f.name }
func (f *fakeProvider) SupportsVision() bool { return false }
func (f *fakeProvider) GenerateWithVision(context.Context, VisionRequest) (string, error) {
	return "", nil
}
func (f *fakeProvider) GenerateText(ctx context.Context, _ TextRequest) (string, error) {
	f.calls++
	if f.hook != nil {
		f.hook()
	}
	if f.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.reply, f.err
}

func captureLog(t *testing.T) *bytes.Buffer {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestValidatedFallsBackOnRejectedContent(t *testing.T) {
	buf := captureLog(t)
	c := NewChain([]Provider{&fakeProvider{name: "one", reply: "BADPROSE"}, &fakeProvider{name: "two", reply: "[1]"}})
	validate := func(s string) error {
		if !strings.HasPrefix(s, "[") {
			return errors.New("not an array")
		}
		return nil
	}
	res, err := c.GenerateTextValidated(context.Background(), TextRequest{}, validate)
	if err != nil || res.Text != "[1]" || res.Provider != "two" {
		t.Fatalf("res %+v err %v", res, err)
	}
	if !strings.Contains(buf.String(), "[ai] text provider one failed: not an array") || strings.Contains(buf.String(), "BADPROSE") {
		t.Fatalf("log %q", buf.String())
	}
}

func TestValidatedStopsWhenContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	first := &fakeProvider{name: "one", err: errors.New("down"), hook: cancel}
	second := &fakeProvider{name: "two", reply: "ok"}
	c := NewChain([]Provider{first, second})
	_, err := c.GenerateTextValidated(ctx, TextRequest{}, func(string) error { return nil })
	if err == nil || second.calls != 0 {
		t.Fatalf("err %v, second calls %d", err, second.calls)
	}
}

func TestValidatedAbandonsAHangingProviderAfterItsDeadline(t *testing.T) {
	old := providerTimeout
	providerTimeout = 50 * time.Millisecond
	defer func() { providerTimeout = old }()
	captureLog(t)
	c := NewChain([]Provider{&fakeProvider{name: "slow", block: true}, &fakeProvider{name: "fast", reply: "ok"}})
	res, err := c.GenerateTextValidated(context.Background(), TextRequest{}, func(string) error { return nil })
	if err != nil || res.Provider != "fast" {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestGenerateTextReturnsProseUnchanged(t *testing.T) {
	c := NewChain([]Provider{&fakeProvider{name: "one", reply: "just some prose"}})
	got, err := c.GenerateText(context.Background(), TextRequest{})
	if err != nil || got != "just some prose" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestValidatedDoesNotAcceptAReplyAfterTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := NewChain([]Provider{&fakeProvider{name: "one", reply: "ok", hook: cancel}})
	if res, err := c.GenerateTextValidated(ctx, TextRequest{}, func(string) error { return nil }); err == nil {
		t.Fatalf("accepted %+v", res)
	}
}
