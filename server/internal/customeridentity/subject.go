package customeridentity

import (
	"encoding/base64"
	"encoding/binary"
)

// ZitadelConnectorID is customer Dex's connector for self-hosted ZITADEL. Dex
// binds it into every portal subject, so changing it needs an identity migration.
const ZitadelConnectorID = "customer-zitadel"

// DexSubject returns the subject claim customer Dex issues for one upstream user
// ID and connector: base64url without padding of Dex's IDTokenSubject protocol buffer,
// with user_id as field 1 and conn_id as field 2 (Dex 2.46.0 GenSubject).
// With no userIDKey, Dex's OIDC connector uses the upstream subject claim, which
// for ZITADEL is the user ID shown in its console.
func DexSubject(userID, connectorID string) string {
	var message []byte
	for field, value := range []string{userID, connectorID} {
		// Proto3 omits empty strings; both fields are length-delimited (wire type 2).
		if value == "" {
			continue
		}
		message = append(message, byte(field+1)<<3|2)
		message = binary.AppendUvarint(message, uint64(len(value)))
		message = append(message, value...)
	}
	return base64.RawURLEncoding.EncodeToString(message)
}
