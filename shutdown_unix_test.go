//go:build unix

package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestServeUntilSignaledDrainsInFlightRequests(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, "done")
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var draining atomic.Bool
	served := make(chan error, 1)
	go func() { served <- serveUntilSignaled(server, listener, &draining) }()

	body := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
		if err != nil {
			body <- err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body <- string(b)
	}()
	<-started

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !draining.Load() {
		if time.Now().After(deadline) {
			t.Fatal("proxy did not start draining after SIGTERM")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-served:
		t.Fatalf("server stopped before the in-flight request finished: %v", err)
	default:
	}
	close(release)

	if got := <-body; got != "done" {
		t.Fatalf("in-flight request was not completed: %q", got)
	}
	if err := <-served; err != nil {
		t.Fatalf("expected a clean shutdown, got %v", err)
	}
}
