package main

import (
	"io"
	"strings"
	"testing"
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
