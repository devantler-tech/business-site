package identity

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// launchLock serializes taking launch places: each order reads the count and
// writes its place under it, so two orders never take the same place and no
// more than the published limit are taken.
const launchLock = 1869820522

// errLaunchChanged refuses an order whose launch discount changed after the
// client saw the price: the last place went in between.
var errLaunchChanged = errors.New("launch discount changed")

// launchPlace is a launch place: taken by an order, or the one an order
// would take now.
type launchPlace struct {
	Place                    int
	Percent                  int64
	ListMinor, DiscountMinor int64
}

func (l launchPlace) ListDKK() int64     { return l.ListMinor / 100 }
func (l launchPlace) DiscountDKK() int64 { return l.DiscountMinor / 100 }
func (l launchPlace) PriceDKK() int64    { return (l.ListMinor - l.DiscountMinor) / 100 }

// LaunchText is the published sentence that explains the offer.
func (l launchPlace) LaunchText(lang string) string {
	if d := offerings.LaunchDiscount; d != nil {
		if lang == "da" {
			return d.Text["da"]
		}
		return d.Text["en"]
	}
	return ""
}

// launchDiscount is what the launch offer takes off a one-time price, in whole
// kroner, or 0 when the price does not qualify: no offer, no one-time price or
// no hosting.
func launchDiscount(oneTimeMinor, monthlyMinor int64) int64 {
	d := offerings.LaunchDiscount
	if d == nil || oneTimeMinor <= 0 || monthlyMinor <= 0 {
		return 0
	}
	discount := (oneTimeMinor*d.Percent + 5000) / 10000 * 100
	if discount <= 0 || discount >= oneTimeMinor {
		return 0
	}
	return discount
}

// openLaunchPlace returns the place an order of the client at these prices
// would take now, or nil: the prices do not qualify, the client already has
// a place, or every place is taken. Only an order holding launchLock may act
// on the answer.
func openLaunchPlace(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, client string, oneTimeMinor, monthlyMinor int64) (*launchPlace, error) {
	discount := launchDiscount(oneTimeMinor, monthlyMinor)
	if discount == 0 {
		return nil, nil
	}
	var taken int
	var held bool
	if err := q.QueryRow(ctx, `SELECT count(*),coalesce(bool_or(client_id=$1),false) FROM portal_launch_places`, client).Scan(&taken, &held); err != nil {
		return nil, errTriageStorage
	}
	if held || taken >= offerings.LaunchDiscount.Limit {
		return nil, nil
	}
	return &launchPlace{Place: taken + 1, Percent: offerings.LaunchDiscount.Percent, ListMinor: oneTimeMinor, DiscountMinor: discount}, nil
}

// lockLaunchPlace takes launchLock for tx and returns the client's open place.
func lockLaunchPlace(ctx context.Context, tx pgx.Tx, client string, oneTimeMinor, monthlyMinor int64) (*launchPlace, error) {
	if launchDiscount(oneTimeMinor, monthlyMinor) == 0 {
		return nil, nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(launchLock)); err != nil {
		return nil, errTriageStorage
	}
	return openLaunchPlace(ctx, tx, client, oneTimeMinor, monthlyMinor)
}

// launchLine adds the full price and the launch discount to a one-time
// invoice or payment line, keeping it at most 200 characters.
func launchLine(line, lang string, l *launchPlace) string {
	if l == nil {
		return line
	}
	suffix := fmt.Sprintf(" — list price %d DKK, %d%% launch discount −%d DKK", l.ListDKK(), l.Percent, l.DiscountDKK())
	if lang == "da" {
		suffix = fmt.Sprintf(" — normalpris %d DKK, lanceringsrabat %d %% −%d DKK", l.ListDKK(), l.Percent, l.DiscountDKK())
	}
	for utf8.RuneCountInString(line)+utf8.RuneCountInString(suffix) > 200 {
		_, size := utf8.DecodeLastRuneInString(line)
		line = line[:len(line)-size]
	}
	return line + suffix
}

// readLaunchOffer returns the launch discount the client's order would get
// now, for the proposal the client can order, or before acceptance the
// shared one. A request with an order has none to offer.
func (s *Foundation) readLaunchOffer(ctx context.Context, p Principal, order *projectOrder, proposals ...*projectProposal) (*launchPlace, error) {
	if order != nil || !s.orderReady(ctx) {
		return nil, nil
	}
	for _, proposal := range proposals {
		if proposal == nil {
			continue
		}
		client, role, err := s.membership(ctx, p)
		if err != nil || role != "client" {
			return nil, ErrDenied
		}
		return openLaunchPlace(ctx, s.pool, client, proposal.OneTimeMinor, proposal.MonthlyMinor)
	}
	return nil, nil
}
