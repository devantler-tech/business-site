package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/jackc/pgx/v5"
)

// queueCalendarCreation records the independent event identity and selected
// destination inside the first confirmation transaction. Configuration supplies
// the private binding, and an empty binding leaves calendar creation disabled.
func (holds appointmentHoldPolicy) queueCalendarCreation(ctx context.Context, tx pgx.Tx, reservation string, revision int) error {
	if holds.calendarBinding == "" {
		return nil
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return errTriageStorage
	}
	_, err := tx.Exec(ctx, `INSERT INTO portal_appointment_calendar_intents
 (reservation_id,revision,calendar_binding,event_key) VALUES($1,$2,$3,$4)`,
		reservation, revision, holds.calendarBinding, hex.EncodeToString(key[:]))
	return creditWriteError(err)
}
