package main

import (
	"strings"
	"testing"
)

func TestPinnedSourceSignatureNeedsSuccessfulExactSigner(t *testing.T) {
	key := sourceSigners[0]
	valid := "[GNUPG:] NEWSIG\n[GNUPG:] VALIDSIG " + key.Signing + " 2026-01-01 1 0 4 0 1 8 01 " + key.Primary + "\n"
	for _, kind := range []string{"valid", "wrong-signing", "wrong-primary", "duplicate", "missing", "failure", "bad-signature"} {
		t.Run(kind, func(t *testing.T) {
			raw := valid
			switch kind {
			case "wrong-signing":
				raw = strings.Replace(raw, key.Signing, strings.Repeat("A", 40), 1)
			case "wrong-primary":
				raw = strings.TrimSuffix(raw, key.Primary+"\n") + strings.Repeat("A", 40) + "\n"
			case "duplicate":
				raw += valid
			case "missing":
				raw = "[GNUPG:] GOODSIG plausible\n"
			case "bad-signature":
				raw += "[GNUPG:] BADSIG rejected\n"
			}
			err := sourcePGPStatus([]byte(raw), key, kind != "failure")
			if (err == nil) != (kind == "valid") {
				t.Fatalf("unexpected signature result: %v", err)
			}
		})
	}
}
