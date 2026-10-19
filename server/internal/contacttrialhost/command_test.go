package contacttrialhost

import (
	"bytes"
	"context"
	"testing"
)

func TestCommandRequiresExplicitDeveloperOptIn(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, 2},
		{[]string{"--private-contact-trial=false"}, 2},
		{[]string{"--private-contact-trial", "extra"}, 2},
		{[]string{"--unknown=confidential-value"}, 2},
		{[]string{"--help"}, 0},
	} {
		var output, diagnostics bytes.Buffer
		if got := Execute(context.Background(), tc.args, nil, &output, &diagnostics); got != tc.code {
			t.Errorf("argument control returned %d, want %d", got, tc.code)
		}
		if bytes.Contains(diagnostics.Bytes(), []byte("confidential-value")) {
			t.Error("argument value leaked into diagnostics")
		}
	}
}
