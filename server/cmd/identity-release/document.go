package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Parse complete bounded JSON while refusing duplicate keys at every depth.
// OCI extension fields remain data; a selected field is decoded into its type.
func sourceJSON(data []byte, into any) error {
	if len(data) == 0 || len(data) > 8<<20 || !utf8.Valid(data) {
		return fmt.Errorf("invalid source metadata bounds or encoding")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	tokens := 0
	var visit func(int) error
	visit = func(depth int) error {
		tokens++
		if depth > 24 || tokens > 100000 {
			return fmt.Errorf("source metadata nesting or token bound")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate source metadata key")
				}
				seen[name] = true
				if err = visit(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return fmt.Errorf("incomplete source object")
			}
		case json.Delim('['):
			for d.More() {
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return fmt.Errorf("incomplete source array")
			}
		case json.Delim('}'), json.Delim(']'):
			return fmt.Errorf("unexpected source delimiter")
		}
		return nil
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing source metadata")
	}
	return json.Unmarshal(data, into)
}

var sourceControlKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
var sourceArchiveName = regexp.MustCompile(`^[A-Za-z0-9_+.-]+$`)

// This checks the original descriptor's source/version and SHA-256 section.
// The retained OpenPGP armor is deliberately not a signature-verification claim.
func checkSourceDescriptor(data []byte, expected sourceDescriptor) error {
	if len(data) == 0 || len(data) > 65536 || !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
		return fmt.Errorf("invalid source descriptor")
	}
	text := string(data)
	const begin = "-----BEGIN PGP SIGNED MESSAGE-----\n"
	if !strings.HasPrefix(text, begin) {
		return fmt.Errorf("original signed descriptor envelope missing")
	}
	text = strings.TrimPrefix(text, begin)
	headerEnd := strings.Index(text, "\n\n")
	if headerEnd < 0 || !strings.HasPrefix(text[:headerEnd], "Hash: ") {
		return fmt.Errorf("descriptor armor header missing")
	}
	text = text[headerEnd+2:]
	const signature = "\n-----BEGIN PGP SIGNATURE-----\n"
	sigStart := strings.Index(text, signature)
	if sigStart < 0 || strings.Count(text, signature) != 1 {
		return fmt.Errorf("descriptor signature envelope missing")
	}
	tail := text[sigStart+len(signature):]
	end := strings.Index(tail, "-----END PGP SIGNATURE-----")
	if end < 1 || strings.TrimSpace(tail[end+len("-----END PGP SIGNATURE-----"):]) != "" {
		return fmt.Errorf("descriptor signature envelope incomplete")
	}
	body := text[:sigStart]
	fields := map[string][]string{}
	current := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimPrefix(line, "- ")
		if line == "" {
			current = ""
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if current == "" {
				return fmt.Errorf("orphan descriptor continuation")
			}
			fields[current] = append(fields[current], strings.TrimSpace(line))
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || !sourceControlKey.MatchString(parts[0]) {
			return fmt.Errorf("malformed source control field")
		}
		if _, ok := fields[parts[0]]; ok {
			return fmt.Errorf("duplicate source control field")
		}
		current = parts[0]
		fields[current] = []string{strings.TrimSpace(parts[1])}
	}
	single := func(key string) string {
		if len(fields[key]) != 1 {
			return ""
		}
		return fields[key][0]
	}
	if single("Source") != expected.Name || single("Version") != expected.Version {
		return fmt.Errorf("descriptor source or version differs")
	}
	lines, ok := fields["Checksums-Sha256"]
	if !ok || len(lines) < 2 || lines[0] != "" {
		return fmt.Errorf("complete SHA-256 descriptor section required")
	}
	observed := map[string]sourceObject{}
	for _, line := range lines[1:] {
		columns := strings.Fields(line)
		if len(columns) != 3 || !sourceDigest.MatchString(columns[0]) || !sourceArchiveName.MatchString(columns[2]) || columns[2] == "." || columns[2] == ".." {
			return fmt.Errorf("invalid source checksum member")
		}
		n, err := strconv.ParseInt(columns[1], 10, 64)
		if err != nil || n < 1 || n > 8<<20 || strconv.FormatInt(n, 10) != columns[1] {
			return fmt.Errorf("invalid source archive length")
		}
		if _, exists := observed[columns[2]]; exists {
			return fmt.Errorf("duplicate source archive checksum")
		}
		observed[columns[2]] = sourceObject{Bytes: n, SHA256: columns[0]}
	}
	if len(observed) != len(expected.Archives) {
		return fmt.Errorf("source archive membership differs")
	}
	for _, want := range expected.Archives {
		got, ok := observed[path.Base(want.Path)]
		if !ok || got.Bytes != want.Bytes || got.SHA256 != want.SHA256 {
			return fmt.Errorf("source archive checksum differs")
		}
	}
	return nil
}
