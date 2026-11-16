package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type confirmationFailure struct{ err error }

func (q confirmationFailure) QueryRow(context.Context, string, ...any) pgx.Row { return q }
func (q confirmationFailure) Scan(...any) error                                { return q.err }

func TestOriginalConfirmationMissingIsDistinctFromStorageFailure(t *testing.T) {
	for _, failure := range []error{pgx.ErrNoRows, errors.New("private database diagnostic")} {
		got, err := readOriginalConfirmation(t.Context(), confirmationFailure{failure}, "request", "client", "acceptance", "order")
		if errors.Is(failure, pgx.ErrNoRows) {
			if err != nil || got.State != confirmationMissing || got.OutboxID != "" || got.SenderAcceptedAt != nil {
				t.Fatal("missing confirmation was not an explicit, empty result")
			}
		} else if err != errTriageStorage || got != (originalConfirmation{}) {
			t.Fatal("database failure leaked diagnostics or returned usable evidence")
		}
	}
}
