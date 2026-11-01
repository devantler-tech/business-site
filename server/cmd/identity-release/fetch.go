package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

type sourceObject struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

var sourceDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var sourceMember = regexp.MustCompile(`^(metadata|layers|sources)/[A-Za-z0-9_+.-]+$`)

// The collector has no authenticated request or ambient proxy/cookie mechanism.
// A redirected registry blob still has to match its exact frozen byte digest.
func sourceHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   60 * time.Second,
		Transport: &http.Transport{Proxy: nil, DisableCompression: true, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, IdleConnTimeout: 30 * time.Second},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" || req.URL.Fragment != "" {
				return fmt.Errorf("unsafe source redirect")
			}
			first := via[0].URL.Hostname()
			target := req.URL.Hostname()
			if target == first {
				return nil
			}
			if first == "gcr.io" && (target == "storage.googleapis.com" || strings.HasSuffix(target, ".pkg.dev")) {
				return nil
			}
			return fmt.Errorf("source redirect left its declared public origin")
		},
	}
}

func validateSourceObject(input sourceObject) (*url.URL, error) {
	u, err := url.Parse(input.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" ||
		(u.Host != "deb.debian.org" && u.Host != "gcr.io") || path.Clean(u.Path) != u.Path || strings.ContainsAny(u.Path, "\\\x00\n\r\t") ||
		!sourceMember.MatchString(input.Path) || path.Clean(input.Path) != input.Path || input.Bytes < 1 || input.Bytes > 8<<20 || !sourceDigest.MatchString(input.SHA256) {
		return nil, fmt.Errorf("invalid frozen public source input")
	}
	return u, nil
}

func readPublicInput(ctx context.Context, client *http.Client, input sourceObject) ([]byte, error) {
	u, err := validateSourceObject(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, input.URL, nil)
	if err != nil {
		return nil, err
	}
	if u.Host == "gcr.io" {
		req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if response.Body == nil {
		return nil, fmt.Errorf("source body missing")
	}
	b, readErr := io.ReadAll(io.LimitReader(response.Body, input.Bytes+1))
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusOK || (response.ContentLength != -1 && response.ContentLength != input.Bytes) || readErr != nil || closeErr != nil || int64(len(b)) != input.Bytes {
		return nil, errors.Join(fmt.Errorf("incomplete public source response"), readErr, closeErr)
	}
	if fmt.Sprintf("%x", sha256.Sum256(b)) != input.SHA256 {
		return nil, fmt.Errorf("public source digest differs")
	}
	return b, nil
}
