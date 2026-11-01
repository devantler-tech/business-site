package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type sourceRoundTrip func(*http.Request) (*http.Response, error)

func (f sourceRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type sourceReadFailure struct{ data []byte }

func (r *sourceReadFailure) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, fmt.Errorf("failed after plausible content")
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}
func (*sourceReadFailure) Close() error { return nil }

type sourceCloseFailure struct{ io.Reader }

func (*sourceCloseFailure) Close() error { return fmt.Errorf("close failed") }

func sourceFetchFixture() sourceObject {
	return sourceObject{Path: "sources/original.tar.xz", URL: "https://deb.debian.org/debian/pool/main/f/fixture/original.tar.xz", Bytes: 4, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("test")))}
}
func TestSourceFetchPreservesCompleteOriginalBytes(t *testing.T) {
	input := sourceFetchFixture()
	client := &http.Client{Transport: sourceRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" {
			t.Fatal("unexpected request authority")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("test")), ContentLength: 4, Header: make(http.Header)}, nil
	})}
	b, err := readPublicInput(context.Background(), client, input)
	if err != nil || string(b) != "test" {
		t.Fatalf("original bytes differ: %v", err)
	}
}
func TestSourceFetchRefusesAlteredOrInaccessibleInput(t *testing.T) {
	for _, kind := range []string{"http-error", "truncated", "oversized", "wrong-hash", "advertised-size", "late-read-error", "close-error", "empty", "huge-bound", "bad-hash", "plain-http", "userinfo", "query", "fragment", "foreign-host", "unclean-path"} {
		t.Run(kind, func(t *testing.T) {
			input := sourceFetchFixture()
			status, body, size := 200, "test", int64(4)
			switch kind {
			case "http-error":
				status = 503
			case "truncated":
				body = "tes"
			case "oversized":
				body = "testx"
			case "wrong-hash":
				body = "nope"
			case "advertised-size":
				size = 5
			case "empty":
				input.Bytes = 0
			case "huge-bound":
				input.Bytes = 9 << 20
			case "bad-hash":
				input.SHA256 = strings.Repeat("A", 64)
			case "plain-http":
				input.URL = "http://deb.debian.org/debian/original.tar.xz"
			case "userinfo":
				input.URL = "https://user:credential@deb.debian.org/debian/original.tar.xz"
			case "query":
				input.URL += "?token=forbidden"
			case "fragment":
				input.URL += "#fragment"
			case "foreign-host":
				input.URL = "https://unrelated.invalid/original.tar.xz"
			case "unclean-path":
				input.Path = "../escape"
			}
			client := &http.Client{Transport: sourceRoundTrip(func(*http.Request) (*http.Response, error) {
				var reader io.ReadCloser = io.NopCloser(strings.NewReader(body))
				if kind == "late-read-error" {
					reader = &sourceReadFailure{data: []byte(body)}
				}
				if kind == "close-error" {
					reader = &sourceCloseFailure{Reader: strings.NewReader(body)}
				}
				return &http.Response{StatusCode: status, Body: reader, ContentLength: size, Header: make(http.Header)}, nil
			})}
			if _, err := readPublicInput(context.Background(), client, input); err == nil {
				t.Fatal("altered or inaccessible original accepted")
			}
		})
	}
}
