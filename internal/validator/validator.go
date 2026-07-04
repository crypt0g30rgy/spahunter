// Package validator verifies that a downloaded asset is actually
// JavaScript and not an HTML SPA-fallback page mistakenly served for a
// stale or guessed bundle URL.
package validator

import (
	"bytes"
)

// Result is the outcome of validating a response body against its
// claimed Content-Type.
type Result struct {
	IsJavaScript bool
	Reason       string
}

var htmlPrefixes = [][]byte{
	[]byte("<!doctype html"),
	[]byte("<!DOCTYPE html"),
	[]byte("<html"),
	[]byte("<HTML"),
}

// Validate inspects the body to decide whether this is genuine JavaScript.
// It prioritizes sniffing for common HTML tags over trusting the Content-Type
// header, making it resilient to misconfigured servers that serve JS with
// a text/html content type.
func Validate(contentType string, body []byte) Result {
	// The strongest signal of a SPA fallback page is the body content itself.
	// If it starts with a common HTML tag, reject it immediately.
	trimmed := bytes.TrimLeft(body, " \t\r\n\ufeff")
	for _, prefix := range htmlPrefixes {
		if len(trimmed) >= len(prefix) && bytes.EqualFold(trimmed[:len(prefix)], prefix) {
			return Result{false, "body begins with HTML marker"}
		}
	}

	// A very short or empty body is suspicious.
	if len(trimmed) == 0 {
		return Result{false, "empty response body"}
	}

	// If the body doesn't look like HTML, we assume it's the JS we asked for,
	// even if the Content-Type is wrong. The pipeline will log the content
	// type mismatch, but we'll still save the file.
	return Result{true, ""}
}
