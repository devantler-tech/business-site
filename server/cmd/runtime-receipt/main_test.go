package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

const fixtureReceipt = `{"repository":"devantler-tech/business-site","sourceRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","publicDigest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","publicProfile":"production-template","image":"ghcr.io/devantler-tech/business-site@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","signerIdentity":"https://github.com/devantler-tech/business-site/.github/workflows/ci.yaml@refs/heads/main","workflowRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","ciRunId":"12345","ciRunAttempt":"2","publicationRunId":"12345","publicationRunAttempt":"2"}`

type member struct {
	name string
	body string
	mode fs.FileMode
}

// members places a chosen receipt in an otherwise valid five-member archive.
func members(receipt string) []member {
	return []member{{"receipt.json", receipt, 0644}, {"runtime.json", `{}`, 0644}, {"signature.json", `[]`, 0644}, {"provenance.json", `{}`, 0644}, {"attestation.json", `[]`, 0644}}
}

// archiveBytes constructs the compressed archive consumed by the real command.
func archiveBytes(t *testing.T, entries []member) []byte {
	t.Helper()
	return archiveMethod(t, entries, zip.Deflate)
}

// archiveMethod permits stored members to isolate compressed-size admission.
func archiveMethod(t *testing.T, entries []member, method uint16) []byte {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: method}
		h.SetMode(entry.mode)
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

// inputs writes a private fixture and binds its actual digest to valid caller options.
func inputs(t *testing.T, data []byte) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.zip")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return []string{"--archive", path, "--sha256", hex.EncodeToString(digest[:]), "--source", strings.Repeat("a", 40), "--run", "12345", "--attempt", "2", "--artifact", "67890"}
}

// TestRuntimeReceiptAcceptsCompleteBoundArchive preserves every admitted receipt field.
func TestRuntimeReceiptAcceptsCompleteBoundArchive(t *testing.T) {
	args := inputs(t, archiveBytes(t, members(fixtureReceipt)))
	var output bytes.Buffer
	if err := run(context.Background(), args, &output); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("receipt missing: %v", err)
	}
	var want map[string]string
	if err := json.Unmarshal([]byte(fixtureReceipt), &want); err != nil {
		t.Fatal(err)
	}
	if len(got) != 11 || got["image"] != want["image"] || got["ciRunAttempt"] != "2" {
		t.Fatal("receipt bindings were lost")
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatal("receipt changed a validated field")
		}
	}
}

// TestRuntimeReceiptRejectsAmbiguousOrUnboundReceipt rejects schema and identity ambiguity.
func TestRuntimeReceiptRejectsAmbiguousOrUnboundReceipt(t *testing.T) {
	for name, receipt := range map[string]string{
		"duplicate":                     strings.Replace(fixtureReceipt, `"repository":`, `"ciRunId":"12345","repository":`, 1),
		"escaped duplicate":             strings.Replace(fixtureReceipt, `"repository":`, `"ciRun\u0049d":"12345","repository":`, 1),
		"case alias":                    strings.Replace(fixtureReceipt, `"ciRunId"`, `"CiRunId"`, 1),
		"unknown":                       strings.Replace(fixtureReceipt, `"repository":`, `"secret":"do-not-print","repository":`, 1),
		"missing":                       strings.Replace(fixtureReceipt, `"ciRunId":"12345",`, "", 1),
		"numeric":                       strings.Replace(fixtureReceipt, `"ciRunId":"12345"`, `"ciRunId":12345`, 1),
		"null":                          strings.Replace(fixtureReceipt, `"ciRunId":"12345"`, `"ciRunId":null`, 1),
		"trailing":                      fixtureReceipt + ` {}`,
		"array":                         `[` + fixtureReceipt + `]`,
		"foreign repository":            strings.ReplaceAll(fixtureReceipt, "devantler-tech/business-site", "other/business-site"),
		"wrong source":                  strings.Replace(fixtureReceipt, strings.Repeat("a", 40), strings.Repeat("d", 40), 1),
		"wrong workflow":                strings.Replace(fixtureReceipt, `"workflowRevision":"`+strings.Repeat("a", 40), `"workflowRevision":"`+strings.Repeat("d", 40), 1),
		"wrong run":                     strings.Replace(fixtureReceipt, `"ciRunId":"12345"`, `"ciRunId":"12346"`, 1),
		"stale attempt":                 strings.Replace(fixtureReceipt, `"ciRunAttempt":"2"`, `"ciRunAttempt":"1"`, 1),
		"different publication":         strings.Replace(fixtureReceipt, `"publicationRunId":"12345"`, `"publicationRunId":"12346"`, 1),
		"different publication attempt": strings.Replace(fixtureReceipt, `"publicationRunAttempt":"2"`, `"publicationRunAttempt":"1"`, 1),
		"wrong profile":                 strings.Replace(fixtureReceipt, "production-template", "default", 1),
		"mutable image":                 strings.Replace(fixtureReceipt, "@sha256:"+strings.Repeat("c", 64), ":latest", 1),
		"wrong signer":                  strings.Replace(fixtureReceipt, "@refs/heads/main", "@refs/heads/topic", 1),
		"malformed digest":              strings.Replace(fixtureReceipt, strings.Repeat("b", 64), strings.Repeat("B", 64), 1),
		"oversized receipt":             strings.Repeat(" ", 16385) + fixtureReceipt,
		"invalid UTF8":                  strings.Replace(fixtureReceipt, "production-template", "production-"+string([]byte{0xff})+"template", 1),
	} {
		t.Run(name, func(t *testing.T) { refuse(t, inputs(t, archiveBytes(t, members(receipt)))) })
	}
}

// refuse asserts the public failure contract without exposing inspected values.
func refuse(t *testing.T, args []string) {
	t.Helper()
	var output bytes.Buffer
	if err := run(context.Background(), args, &output); err == nil {
		t.Error("invalid input was admitted")
	}
	if output.Len() != 0 {
		t.Error("failure exposed output")
	}
}

// TestRuntimeReceiptRejectsInvalidArchives isolates member, stream and size failures.
func TestRuntimeReceiptRejectsInvalidArchives(t *testing.T) {
	for _, name := range []string{"extra", "missing", "duplicate", "traversal", "absolute", "directory", "symlink", "fifo", "expanded", "total expanded", "non-receipt JSON", "corrupt", "truncated", "not zip", "compressed"} {
		t.Run(name, func(t *testing.T) {
			entries := members(fixtureReceipt)
			switch name {
			case "extra":
				entries = append(entries, member{"extra.json", `{}`, 0644})
			case "missing":
				entries = entries[:4]
			case "duplicate":
				entries[1].name = "receipt.json"
			case "traversal":
				entries[1].name = "../runtime.json"
			case "absolute":
				entries[1].name = "/runtime.json"
			case "directory":
				entries[1].mode = fs.ModeDir | 0755
			case "symlink":
				entries[1].mode = fs.ModeSymlink | 0777
			case "fifo":
				entries[1].mode = fs.ModeNamedPipe | 0600
			case "expanded":
				entries[1].body = `"` + strings.Repeat("x", 2*1024*1024) + `"`
			case "total expanded":
				for i := 1; i < len(entries); i++ {
					entries[i].body = `"` + strings.Repeat("x", 2*1024*1024-2) + `"`
				}
			case "non-receipt JSON":
				entries[1].body = `{} {}`
			case "compressed":
				entries[1].body = `"` + strings.Repeat("x", 2*1024*1024-2) + `"`
				entries[2].body = entries[1].body
			}
			data := archiveBytes(t, entries)
			switch name {
			case "corrupt":
				// Alter the central-directory CRC of a non-receipt member; all streams must be examined.
				index := bytes.Index(data, []byte{'P', 'K', 1, 2})
				index += bytes.Index(data[index+4:], []byte{'P', 'K', 1, 2}) + 4
				binary.LittleEndian.PutUint32(data[index+16:], 0)
			case "truncated":
				data = data[:len(data)-10]
			case "not zip":
				data = []byte("not a zip")
			case "compressed":
				data = archiveMethod(t, entries, zip.Store)
				if _, err := archiveReceipt(data); err != nil {
					t.Fatal("compressed-limit fixture must otherwise be a valid complete archive")
				}
			}
			refuse(t, inputs(t, data))
		})
	}
}

// TestRuntimeReceiptRejectsInvalidCallerInputs checks generic refusal and cancellation.
func TestRuntimeReceiptRejectsInvalidCallerInputs(t *testing.T) {
	for _, index := range []int{3, 5, 7, 9, 11} {
		for _, invalid := range []string{"", "0", "01", "1e2", "-1", "1suffix", "9007199254740992", "do-not-print"} {
			t.Run(strconv.Itoa(index)+invalid, func(t *testing.T) {
				args := inputs(t, archiveBytes(t, members(fixtureReceipt)))
				args[index] = invalid
				refuse(t, args)
			})
		}
	}
	args := inputs(t, archiveBytes(t, members(fixtureReceipt)))
	args[3] = strings.Repeat("0", 64)
	refuse(t, args)
	refuse(t, inputs(t, archiveBytes(t, members(fixtureReceipt)))[:10])
	duplicate := inputs(t, archiveBytes(t, members(fixtureReceipt)))
	duplicate[0] = "--sha256"
	refuse(t, duplicate)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	if err := run(ctx, inputs(t, archiveBytes(t, members(fixtureReceipt))), &output); err == nil || output.Len() != 0 {
		t.Fatal("cancelled input admitted")
	}
}

// TestRuntimeReceiptParserGuards reaches each parser guard before archive admission.
func TestRuntimeReceiptParserGuards(t *testing.T) {
	valid := []string{"--archive", "unused.zip", "--sha256", strings.Repeat("b", 64), "--source", strings.Repeat("a", 40), "--run", "12345", "--attempt", "2", "--artifact", "67890"}
	if _, err := arguments(valid); err != nil {
		t.Fatal("complete caller options were refused")
	}
	t.Run("odd argument count", func(t *testing.T) {
		if _, err := arguments(valid[:11]); err == nil {
			t.Fatal("odd argument list was admitted")
		}
	})
	t.Run("duplicate option", func(t *testing.T) {
		duplicate := append([]string(nil), valid...)
		// Retain all validated scalar values: only the archive option is displaced.
		duplicate[0] = "--sha256"
		if _, err := arguments(duplicate); err == nil {
			t.Fatal("duplicate option was admitted")
		}
	})
}

// TestRuntimeReceiptRejectsNonRegularPaths exercises descriptor-level path refusal.
func TestRuntimeReceiptRejectsNonRegularPaths(t *testing.T) {
	args := inputs(t, archiveBytes(t, members(fixtureReceipt)))
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(args[1], link); err != nil {
		t.Fatal(err)
	}
	args[1] = link
	refuse(t, args)
	args[1] = t.TempDir()
	refuse(t, args)
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	args[1] = fifo
	refuse(t, args)
	args[1] = filepath.Join(t.TempDir(), "absent")
	refuse(t, args)
}

// TestRuntimeReceiptAdmitsLargestSafeArtifactID keeps the numeric ceiling inclusive.
func TestRuntimeReceiptAdmitsLargestSafeArtifactID(t *testing.T) {
	args := inputs(t, archiveBytes(t, members(fixtureReceipt)))
	args[11] = "9007199254740991"
	var output bytes.Buffer
	if err := run(context.Background(), args, &output); err != nil || output.Len() == 0 {
		t.Fatal("largest safe positive artifact ID refused")
	}
}

// TestRuntimeReceiptCompiledCommand observes actual stdout, stderr and exit status.
func TestRuntimeReceiptCompiledCommand(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "runtime-receipt")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binaryPath, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, output)
	}
	args := inputs(t, archiveBytes(t, members(fixtureReceipt)))
	output, err := exec.CommandContext(t.Context(), binaryPath, args...).Output()
	if err != nil || !bytes.Contains(output, []byte(`"ciRunId":"12345"`)) {
		t.Fatal("compiled command did not admit valid input")
	}
	args[11] = "do-not-print"
	bad := exec.CommandContext(t.Context(), binaryPath, args...)
	var stdout, stderr bytes.Buffer
	bad.Stdout = &stdout
	bad.Stderr = &stderr
	if err := bad.Run(); err == nil || stdout.Len() != 0 || stderr.String() != "runtime-receipt refused\n" {
		t.Fatal("compiled rejection leaked input or success")
	}
}
