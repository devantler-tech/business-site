package identity

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
)

// Keep the public observation deliberately smaller than private evidence. Adding
// a domain field later cannot accidentally publish an agreement or payment fact.
type operatorWorkObservation struct {
	Request    string               `json:"requestId"`
	Approval   string               `json:"approvalId,omitempty"`
	Generation int64                `json:"approvalGeneration"`
	Submitted  time.Time            `json:"submittedAt"`
	ObservedAt time.Time            `json:"observedAt"`
	State      workEligibilityState `json:"state"`
}

type operatorWorkEligibility struct {
	Items []operatorWorkObservation `json:"items"`
	Next  string                    `json:"nextCursor,omitempty"`
}

func (b *browser) workEligibility(ctx context.Context, r *http.Request) (any, error) {
	// This action has no administrative machine grant. Bind the original human
	// operator from the shared dispatcher, never a query parameter or new session.
	bound, ok := ctx.Value(operatorAuthorityKey{}).(*operatorAuthority)
	if !ok || bound.kind != "operator" {
		return nil, ErrDenied
	}
	cursor, err := eligibilityCursor(r.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	page, err := b.foundation.readWorkEligibilityChecked(ctx, bound.principal, cursor, func(ctx context.Context, tx pgx.Tx, _ string) error {
		if !b.operatorMutationAllowed(ctx, tx) {
			return ErrDenied
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	response := operatorWorkEligibility{Items: make([]operatorWorkObservation, 0, len(page.Items))}
	for _, item := range page.Items {
		response.Items = append(response.Items, operatorWorkObservation{Request: item.Request, Approval: item.Approval, Generation: item.Generation, Submitted: item.Submitted.UTC(), ObservedAt: item.ObservedAt.UTC(), State: item.State})
	}
	if page.Next.ID != "" {
		response.Next = encodeTriageCursor(projectRequest{ID: page.Next.ID, Submitted: page.Next.Submitted})
	}
	return response, nil
}

func eligibilityCursor(raw string) (triageCursor, error) {
	if len(raw) > 512 {
		return triageCursor{}, errOperatorInput
	}
	query, err := url.ParseQuery(raw)
	if err != nil {
		return triageCursor{}, errOperatorInput
	}
	if len(query) == 0 {
		return triageCursor{}, nil
	}
	values, exists := query["cursor"]
	if len(query) != 1 || !exists || len(values) != 1 {
		return triageCursor{}, errOperatorInput
	}
	cursor, valid := parseTriageCursor(values[0])
	if !valid {
		return triageCursor{}, errOperatorInput
	}
	return cursor, nil
}
