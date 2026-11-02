// Command identity-release explicitly retains and verifies the frozen identity
// base's original sources. It has no image publisher or activation operation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"
)

const releaseUsage = `Explicit source-data operations (ordinary invocation is inert):
  identity-release capture-base-source --output /new/private/capsule
  identity-release verify-base-source --input /private/capsule --sha256 <external-manifest-sha256>
  identity-release pack-base-source --input /private/capsule --sha256 <external-manifest-sha256> --output /new/base-source.tar
  identity-release prepare-handoff --input /native/job-root --source-sha <commit> --run-id <run> --run-attempt <attempt> --output /new/handoff.json
No URL/profile override, authenticated request, image publication or portal activation is supported.
`

func runRelease(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		_, err := io.WriteString(out, releaseUsage)
		return err
	}
	op := args[0]
	if op == "prepare-handoff" {
		return runHandoff(args[1:], out)
	}
	if op != "capture-base-source" && op != "verify-base-source" && op != "pack-base-source" {
		return fmt.Errorf("unknown explicit source operation")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return fmt.Errorf("collector requires verified Linux or Darwin file metadata")
	}
	f := flag.NewFlagSet(op, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var input, seal, output string
	if op != "capture-base-source" {
		f.StringVar(&input, "input", "", "physical private capsule")
		f.StringVar(&seal, "sha256", "", "external manifest seal")
	}
	if op != "verify-base-source" {
		f.StringVar(&output, "output", "", "new physical destination")
	}
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected source arguments")
	}
	if op != "capture-base-source" && (input == "" || !sourceDigest.MatchString(seal)) {
		return fmt.Errorf("input and external SHA-256 required")
	}
	if op != "verify-base-source" {
		if _, err := sourceOutputPath(output); err != nil {
			return err
		}
	}
	p, err := loadBaseProfile()
	if err != nil {
		return err
	}
	var archiveSHA string
	switch op {
	case "capture-base-source":
		ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		client := sourceHTTPClient()
		defer client.CloseIdleConnections()
		seal, err = captureBaseCapsule(ctx, p, func(ctx context.Context, s sourceObject) ([]byte, error) { return readPublicInput(ctx, client, s) }, output)
	case "verify-base-source":
		_, err = verifyBaseCapsule(p, input, seal)
	case "pack-base-source":
		archiveSHA, err = packBaseCapsule(p, input, seal, output)
	}
	if err != nil {
		return err
	}
	receipt := struct {
		SchemaVersion              int    `json:"schemaVersion"`
		Operation                  string `json:"operation"`
		BaseImage                  string `json:"baseImage"`
		ManifestSHA256             string `json:"manifestSha256"`
		ArchiveSHA256              string `json:"archiveSha256,omitempty"`
		PayloadFiles               int    `json:"payloadFiles"`
		BaseImageSignatureVerified bool   `json:"baseImageSignatureVerified"`
		OpenPGPVerified            bool   `json:"openpgpVerified"`
		SourceReleaseVerified      bool   `json:"sourceReleaseVerified"`
		ImageAdmission             bool   `json:"imageAdmission"`
	}{SchemaVersion: 1, Operation: op, BaseImage: p.Image, ManifestSHA256: seal, ArchiveSHA256: archiveSHA, PayloadFiles: 41}
	return json.NewEncoder(out).Encode(receipt)
}

func main() {
	if err := runRelease(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "identity-release:", err)
		os.Exit(1)
	}
}
