package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type sourceSigner struct {
	File                     string
	Bytes                    int64
	SHA256, Signing, Primary string
}

// Public certificate bytes at Debian keyring revision
// 83d66888437c84e460765fc4f88d5cd7ceedfbc4. This declaration selects the
// historical signer for each frozen descriptor, not today's certificate status.
var sourceSigners = []sourceSigner{
	{"0x41CE7F0B9F1B8B32", 10961, "d808fed741248cfa3cbf2d732c811ba5cc4b400de857faff3a2488f489eb87b3", "D54C3BFAFFB042DE382DA5D741CE7F0B9F1B8B32", "D54C3BFAFFB042DE382DA5D741CE7F0B9F1B8B32"},
	{"0xE6FFF1E38DC968B0", 18419, "6901ec93431fa4a0755d3e1510ff1b53f7cd63d5e70b5c0c799159d652bf1840", "272945CD836D38DFB7427E86CB3EC33AE1DED781", "6791403B68AE2690517C42EAE6FFF1E38DC968B0"},
	{"0xBA9C78061DDD8C9B", 96346, "dcc332064e4830bf39e2e140b609af12d9779ed26837c7a2964ea0e329bc904e", "52BC8695BE34F90AD7D40CB81388C0F899E8336B", "77462642A9EF94FD0F77196DBA9C78061DDD8C9B"},
	{"0xC5BD6C8F2295D502", 11036, "c90474c51f054f80456469b95f6529410e2997d2643c82a988a5cf8011adedd3", "73471499CC60ED9EEE805946C5BD6C8F2295D502", "73471499CC60ED9EEE805946C5BD6C8F2295D502"},
	{"0x310180050905E40C", 63002, "da7fa00bcc69561d4414243edcbfd64b83dd9a289969ac94cbef57a3664c811f", "55781DAB34E61A09EFB88BE19DB0235556F8CFAD", "7B27A3F1A6E18CD9588B4AE8310180050905E40C"},
}

// sourcePGPStatus requires successful verification and one exact frozen signing
// and primary-key pair; it does not establish current certificate validity.
func sourcePGPStatus(raw []byte, key sourceSigner, success bool) error {
	if !success || len(raw) > 1<<20 {
		return fmt.Errorf("descriptor signature command failed")
	}
	count := 0
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "[GNUPG:]" {
			continue
		}
		switch fields[1] {
		case "VALIDSIG":
			if len(fields) != 12 || fields[2] != key.Signing || fields[11] != key.Primary {
				return fmt.Errorf("frozen descriptor signer differs")
			}
			count++
		case "BADSIG", "ERRSIG", "REVKEYSIG", "NODATA", "FAILURE":
			return fmt.Errorf("descriptor signature refusal")
		}
	}
	if count != 1 {
		return fmt.Errorf("one exact descriptor signature required")
	}
	return nil
}

type authBoundedBuffer struct{ bytes.Buffer }

// Write bounds each verifier stream before it can accumulate unbounded output.
func (b *authBoundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, fmt.Errorf("signature output bound exceeded")
	}
	return b.Buffer.Write(p)
}

// sourceAuthCommand caps a verifier's lifetime and both output streams while
// keeping diagnostic text separate from the bytes interpreted as a verdict.
func sourceAuthCommand(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out, diagnostic authBoundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out.Bytes(), err
}

// runBaseAuth verifies frozen descriptor and image signatures from the admitted
// capsule, keeping these observations separate from image-admission authority.
func runBaseAuth(ctx context.Context, args []string, out io.Writer) (resultErr error) {
	f := flag.NewFlagSet("verify-base-auth", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	input, seal, repo, output := f.String("input", "", "verified original base"), f.String("sha256", "", "external base manifest seal"), f.String("repository", "", "trusted caller source"), f.String("output", "", "new private signature evidence")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected base authentication argument")
	}
	p, err := loadBaseProfile()
	if err != nil {
		return err
	}
	if _, err = verifyBaseCapsule(p, *input, *seal); err != nil {
		return err
	}
	if len(p.Descriptors) != len(sourceSigners) {
		return fmt.Errorf("frozen signer inventory differs")
	}
	destination, err := sourceOutputPath(*output)
	if err != nil {
		return err
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return err
	}
	capsule, err := os.OpenRoot(*input)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, capsule.Close()) }()
	repository, err := os.OpenRoot(*repo)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, repository.Close()) }()
	for i, key := range sourceSigners {
		original, err := readHandoffSnapshot(repository, "third_party/debian-source-signers/"+key.File+".pgp", 1<<20, true, 0644)
		if err != nil || original.file.Bytes != key.Bytes || original.file.SHA256 != key.SHA256 {
			return fmt.Errorf("pinned public signer certificate differs")
		}
		keyPath := filepath.Join(destination, key.File+".pgp")
		if err = os.WriteFile(keyPath, original.data, 0600); err != nil {
			return err
		}
		descriptor := p.Descriptors[i].Descriptor
		raw, err := readSourceFile(capsule, sourceFile{Path: descriptor.Path, Bytes: descriptor.Bytes, SHA256: descriptor.SHA256, Mode: 0444})
		if err != nil {
			return err
		}
		status, commandErr := sourceAuthCommand(ctx, raw, "gpgv", "--homedir", destination, "--keyring", keyPath, "--status-fd=1", "-")
		if err = sourcePGPStatus(status, key, commandErr == nil); err != nil {
			return err
		}
	}
	signature, err := sourceAuthCommand(ctx, nil, "cosign", "verify", "--certificate-oidc-issuer", "https://accounts.google.com", "--certificate-identity", "keyless@distroless.iam.gserviceaccount.com", p.Image)
	if err != nil {
		return fmt.Errorf("frozen Distroless signature verification failed")
	}
	var objects []json.RawMessage
	if err = sourceJSON(signature, &objects); err != nil || len(objects) == 0 {
		return fmt.Errorf("complete Distroless verification result required")
	}
	// These are new observations. Never rewrite the producer capsule's false
	// signature/admission fields, and never imply current certificate validity.
	receipt := struct {
		SchemaVersion                         int    `json:"schemaVersion"`
		Profile                               string `json:"profile"`
		BaseManifestSHA256                    string `json:"baseManifestSha256"`
		FrozenDescriptorSignatures            int    `json:"frozenDescriptorSignatures"`
		BaseImageSignatureVerified            bool   `json:"baseImageSignatureVerified"`
		CurrentCertificateValidityEstablished bool   `json:"currentCertificateValidityEstablished"`
		ImageAdmission                        bool   `json:"imageAdmission"`
		PortalActivated                       bool   `json:"portalActivated"`
	}{1, "identity-frozen-base-auth-v1", *seal, 5, true, false, false, false}
	return json.NewEncoder(out).Encode(receipt)
}
