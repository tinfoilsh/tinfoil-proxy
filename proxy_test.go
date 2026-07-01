package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func TestExtractTokenUsageSupportsChatUsage(t *testing.T) {
	upstreamed, downstreamed, ok := extractTokenUsage([]byte(`{"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`))
	if !ok {
		t.Fatal("expected usage to be detected")
	}
	if upstreamed != 11 || downstreamed != 7 {
		t.Fatalf("expected 11 upstreamed and 7 downstreamed, got %d and %d", upstreamed, downstreamed)
	}
}

func TestExtractTokenUsageSupportsResponsesUsage(t *testing.T) {
	upstreamed, downstreamed, ok := extractTokenUsage([]byte(`{"usage":{"input_tokens":13,"output_tokens":5}}`))
	if !ok {
		t.Fatal("expected usage to be detected")
	}
	if upstreamed != 13 || downstreamed != 5 {
		t.Fatalf("expected 13 upstreamed and 5 downstreamed, got %d and %d", upstreamed, downstreamed)
	}
}

func TestUsageTrackingBodyEmitsNonStreamingUsage(t *testing.T) {
	var emitted []tokenStatsMessage
	counter := newTokenCounter(func(msg tokenStatsMessage) error {
		emitted = append(emitted, msg)
		return nil
	})
	body := newUsageTrackingBody(
		io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":3,"completion_tokens":2}}`)),
		"application/json",
		counter,
	)

	if _, err := io.Copy(io.Discard, body); err != nil {
		t.Fatal(err)
	}

	assertTokenStats(t, emitted, 3, 2)
}

func TestStreamParserEmitsUsageDeltas(t *testing.T) {
	var emitted []tokenStatsMessage
	counter := newTokenCounter(func(msg tokenStatsMessage) error {
		emitted = append(emitted, msg)
		return nil
	})
	parser := tokenUsageParser{
		stream: true,
		usage:  responseTokenUsage{counter: counter},
	}

	parser.write([]byte("data: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\n"))
	parser.write([]byte("data: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":4}}\n\n"))
	parser.write([]byte("data: [DONE]\n\n"))
	parser.finalize()

	assertTokenStats(t, emitted, 3, 4)
}

func TestEnsureStreamUsageIncludedAddsRequestOption(t *testing.T) {
	req := httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1:3301/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-oss-120b","stream":true,"messages":[]}`),
	)
	req.Header.Set("Content-Type", "application/json")

	if err := ensureStreamUsageIncluded(req); err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	streamOptions, ok := payload["stream_options"].(map[string]any)
	if !ok {
		t.Fatal("expected stream_options to be added")
	}
	if streamOptions["include_usage"] != true {
		t.Fatalf("expected include_usage to be true, got %v", streamOptions["include_usage"])
	}
	if req.ContentLength <= 0 {
		t.Fatal("expected content length to be updated")
	}
}

func TestEnsureStreamUsageIncludedSkipsNonStreamingRequest(t *testing.T) {
	body := `{"model":"gpt-oss-120b","messages":[]}`
	req := httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1:3301/v1/chat/completions",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	if err := ensureStreamUsageIncluded(req); err != nil {
		t.Fatal(err)
	}
	updated, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != body {
		t.Fatalf("expected body to stay unchanged, got %s", string(updated))
	}
}

func TestEnsureStreamUsageIncludedPreservesExplicitIncludeUsage(t *testing.T) {
	body := `{"model":"gpt-oss-120b","stream":true,"messages":[],"stream_options":{"include_usage":false}}`
	req := httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1:3301/v1/chat/completions",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	if err := ensureStreamUsageIncluded(req); err != nil {
		t.Fatal(err)
	}
	updated, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != body {
		t.Fatalf("expected body to stay unchanged, got %s", string(updated))
	}
}

func TestLoggingTransportDoesNotProxyAfterUsageRequestReadError(t *testing.T) {
	readErr := errors.New("partial read")
	req := httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1:3301/v1/chat/completions",
		errReaderCloser{data: []byte(`{"model":"gpt-oss-120b","stream":true`), err: readErr},
	)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(`{"model":"gpt-oss-120b","stream":true`))

	transport := &recordingRoundTripper{}
	lt := withLoggingTransport(log.StandardLogger(), transport, newTokenCounter(nil))
	resp, err := lt.RoundTrip(req)
	if !errors.Is(err, readErr) {
		t.Fatalf("expected read error, got response=%v error=%v", resp, err)
	}
	if transport.called {
		t.Fatal("expected upstream transport not to be called")
	}
}

func TestTokenCounterEmitsMonotonicTotals(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{}, 1)
	done := make(chan struct{})
	var emitted []tokenStatsMessage

	counter := newTokenCounter(func(msg tokenStatsMessage) error {
		if len(emitted) == 0 {
			close(firstStarted)
			<-releaseFirst
		} else {
			secondStarted <- struct{}{}
		}
		emitted = append(emitted, msg)
		return nil
	})

	go func() {
		counter.add(1, 0)
		close(done)
	}()

	<-firstStarted
	secondDone := make(chan struct{})
	go func() {
		counter.add(2, 0)
		close(secondDone)
	}()

	select {
	case <-secondStarted:
		t.Fatal("second emit started before first emit completed")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseFirst)
	<-done
	<-secondDone

	if len(emitted) != 2 {
		t.Fatalf("expected two emissions, got %d", len(emitted))
	}
	if emitted[0].Upstreamed != 1 || emitted[1].Upstreamed != 3 {
		t.Fatalf("expected monotonic upstream totals [1, 3], got [%d, %d]", emitted[0].Upstreamed, emitted[1].Upstreamed)
	}
}

func assertTokenStats(t *testing.T, emitted []tokenStatsMessage, upstreamed, downstreamed uint64) {
	t.Helper()
	if len(emitted) == 0 {
		t.Fatal("expected token stats to be emitted")
	}
	last := emitted[len(emitted)-1]
	if last.Upstreamed != upstreamed || last.Downstreamed != downstreamed {
		t.Fatalf("expected %d upstreamed and %d downstreamed, got %d and %d", upstreamed, downstreamed, last.Upstreamed, last.Downstreamed)
	}
}

type errReaderCloser struct {
	data []byte
	err  error
}

func (r errReaderCloser) Read(p []byte) (int, error) {
	copy(p, r.data)
	return len(r.data), r.err
}

func (r errReaderCloser) Close() error {
	return nil
}

type recordingRoundTripper struct {
	called bool
}

func (r *recordingRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	r.called = true
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}
