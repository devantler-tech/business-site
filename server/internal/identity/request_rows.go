package identity

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

// scanProjectRequests decodes rows only; each caller retains its authority,
// query scope, resource cleanup and client-facing error policy.
func scanProjectRequests(rows pgx.Rows) ([]projectRequest, error) {
	var result []projectRequest
	for rows.Next() {
		var d projectRequest
		if err := rows.Scan(&d.ID, &d.Product, &d.Title, &d.Brief, &d.Submitted); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func missingProjectRequest(id string, rows []projectRequest, err error) bool {
	return errors.Is(err, pgx.ErrNoRows) || (err == nil && id != "" && len(rows) == 0)
}
