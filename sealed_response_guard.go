package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	ehbpprotocol "github.com/tinfoilsh/encrypted-http-body-protocol/protocol"
)

const unauthenticatedResponseErrorType = "tinfoil_proxy_unauthenticated_response"

// sealedResponseGuard withholds responses the enclave did not seal. EHBP lets
// a non-2xx response without a response nonce through as unauthenticated
// plaintext, which anything between the proxy and the enclave can forge. The
// enclave seals its own errors, so for a request whose body was sealed such a
// response did not come from it: the local client gets the status and a
// proxy-written body, never the upstream headers or body.
//
// A request without a body is not sealed, so EHBP cannot authenticate its
// response and it passes through unchanged.
type sealedResponseGuard struct {
	transport http.RoundTripper
}

func (g *sealedResponseGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	// EHBP seals exactly the requests that carry a body.
	sealed := req.Body != nil && req.Body != http.NoBody
	resp, err := g.transport.RoundTrip(req)
	if err != nil || !sealed || resp.Header.Get(ehbpprotocol.ResponseNonceHeader) != "" {
		return resp, err
	}
	resp.Body.Close()
	return unauthenticatedResponse(req, resp.StatusCode), nil
}

func unauthenticatedResponse(req *http.Request, status int) *http.Response {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	payload.Error.Message = fmt.Sprintf("an intermediary answered with status %d before the request reached the verified enclave; its response was withheld", status)
	payload.Error.Type = unauthenticatedResponseErrorType
	// This string-only struct has an infallible JSON encoding.
	body, _ := json.Marshal(payload)
	return &http.Response{
		Status:     strconv.Itoa(status) + " " + http.StatusText(status),
		StatusCode: status,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header: http.Header{
			"Content-Type":   {"application/json"},
			"Content-Length": {strconv.Itoa(len(body))},
		},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
