package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type partialReader struct{}

// Read simulates bytes followed by a real read failure in the same call.
func (partialReader) Read(p []byte) (int, error) { return copy(p, "partial"), io.ErrUnexpectedEOF }

type shortWriter struct{}

// Write simulates an incorrectly short successful filesystem write.
func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

// TestCopyStreamFailures checks I/O behavior used by real capture and readback.
func TestCopyStreamFailures(t *testing.T) {
	if _, _, err := copyStream(io.Discard, partialReader{}, 64); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("partial read accepted: %v", err)
	}
	if _, _, err := copyStream(shortWriter{}, bytes.NewBufferString("payload"), 64); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write accepted: %v", err)
	}
	if _, _, err := copyStream(io.Discard, bytes.NewBufferString("too long"), 3); err == nil {
		t.Fatal("size limit ignored")
	}
	var dst bytes.Buffer
	n, sum, err := copyStream(&dst, bytes.NewBufferString("complete"), 8)
	if err != nil || n != 8 || sum != digest(dst.Bytes()) {
		t.Fatalf("complete copy: n=%d sum=%s err=%v", n, sum, err)
	}
}

// TestMetadataDepth bounds malicious nested metadata before recursive decoding.
func TestMetadataDepth(t *testing.T) {
	if err := uniqueJSON([]byte(strings.Repeat("[", 128) + "0" + strings.Repeat("]", 128))); err == nil {
		t.Fatal("unbounded nested metadata accepted")
	}
}

// TestCommandModes keeps ordinary invocation inert and rejects partial/mixed commands.
func TestCommandModes(t *testing.T) {
	var out bytes.Buffer
	if err := run(nil, &out); err != nil || out.Len() != 0 {
		t.Fatalf("ordinary invocation: %q %v", out.String(), err)
	}
	for _, args := range [][]string{{"--capture"}, {"--verify", "missing"}, {"--output", "unused"}, {"--capture=false"}, {"--capture", "--verify", "mixed"}, {"--unknown"}, {"unexpected"}} {
		if err := run(args, &out); err == nil || out.Len() != 0 {
			t.Fatalf("partial command accepted: %v, %v", args, err)
		}
	}
}
