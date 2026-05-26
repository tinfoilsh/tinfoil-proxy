package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/tinfoilsh/tinfoil-go"
)

const (
	httpReadHeaderTimeout = 30 * time.Second
	httpIdleTimeout       = 120 * time.Second
	httpMaxHeaderBytes    = 1 << 20
	handshakeTimeout      = 60 * time.Second
)

type readyMessage struct {
	Event   string `json:"event"`
	Enclave string `json:"enclave"`
	Repo    string `json:"repo"`
	Listen  string `json:"listen"`
}

func emitReady(enclave, repo, listen string) error {
	msg := readyMessage{
		Event:   "ready",
		Enclave: enclave,
		Repo:    repo,
		Listen:  listen,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(os.Stdout, string(payload)); err != nil {
		return err
	}
	return nil
}

func waitForGoSignal(timeout time.Duration) error {
	result := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			result <- fmt.Errorf("handshake stdin closed: %w", err)
			return
		}
		switch strings.TrimSpace(line) {
		case "go":
			result <- nil
		case "abort":
			result <- errors.New("aborted by parent")
		default:
			result <- fmt.Errorf("unexpected handshake signal %q", strings.TrimSpace(line))
		}
	}()
	select {
	case err := <-result:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("handshake signal not received within %s", timeout)
	}
}

func runProxy(cmd *cobra.Command, args []string) error {
	setupLogger()
	warnIfNonLoopbackBind()

	log.WithFields(log.Fields{
		"enclave_host": enclaveHost,
		"repo":         repo,
	}).Info("initializing secure client")

	var tinfoilClient *tinfoil.Client
	var err error
	if enclaveHost == "" && repo == "" {
		tinfoilClient, err = tinfoil.NewClient()
		if err == nil {
			enclaveHost = tinfoilClient.Enclave()
			repo = tinfoilClient.Repo()
		}
	} else {
		tinfoilClient, err = tinfoil.NewClientWithParams(enclaveHost, repo)
	}
	if err != nil {
		log.WithError(err).Error("failed to create HTTP client")
		return err
	}
	log.Debug("secure HTTP client created successfully")

	targetURL, err := url.Parse("https://" + enclaveHost)
	if err != nil {
		log.WithError(err).Error("failed to parse upstream URL")
		return err
	}

	httpClient := tinfoilClient.HTTPClient()

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = withLoggingTransport(log.StandardLogger(), httpClient.Transport)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})

	addr := bindAddress()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.WithError(err).Error("failed to bind listener")
		return err
	}

	if handshake {
		if err := emitReady(enclaveHost, repo, addr); err != nil {
			listener.Close()
			return err
		}
		if err := waitForGoSignal(handshakeTimeout); err != nil {
			listener.Close()
			return err
		}
	}

	log.WithFields(log.Fields{
		"address":      addr,
		"enclave_host": enclaveHost,
	}).Info("starting HTTP proxy server")
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}
	return server.Serve(listener)
}

func setupLogger() {
	if trace {
		log.SetLevel(log.TraceLevel)
	} else if verbose {
		log.SetLevel(log.InfoLevel)
	}
	if logFormat == "json" {
		log.SetFormatter(&log.JSONFormatter{})
	} else {
		log.SetFormatter(&log.TextFormatter{})
	}
}

func withLoggingTransport(logger *log.Logger, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &loggingTransport{
		wrapped: base,
		logger:  logger,
	}
}

// loggingTransport implements http.RoundTripper and wraps an existing
// transport with logging functions
type loggingTransport struct {
	wrapped http.RoundTripper
	logger  *log.Logger
}

func (lt *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	lt.logger.WithFields(log.Fields{
		"method": req.Method,
		"host":   req.URL.Host,
		"path":   req.URL.Path,
	}).Debug("Outgoing request to upstream")

	resp, err := lt.wrapped.RoundTrip(req)
	if err != nil {
		lt.logger.WithFields(log.Fields{
			"method": req.Method,
			"host":   req.URL.Host,
			"path":   req.URL.Path,
		}).Error("Request to upstream failed")
		return nil, err
	}

	logEntry := lt.logger.WithFields(log.Fields{
		"method": req.Method,
		"target": req.URL.Host,
		"path":   req.URL.Path,
		"status": resp.Status,
		"size":   resp.ContentLength,
	})

	switch {
	case resp.StatusCode >= 500:
		logEntry.Warn("Upstream server error")
	case resp.StatusCode >= 400:
		logEntry.Warn("Upstream client error")
	default:
		logEntry.Info("Upstream request complete")
	}

	return resp, err
}
