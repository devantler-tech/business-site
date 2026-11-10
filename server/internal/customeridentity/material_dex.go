package customeridentity

import (
	"bytes"
	"encoding/json"
	"regexp"
)

const dexServePhase = "dex-serve"

var dexClientID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,127}$`)

// materialDexDocuments prepares a closed native broker using only its serving authority.
func materialDexDocuments(endpoints Endpoints, settings map[string]string, files map[string][]byte) (map[string][]byte, error) {
	databaseSecret := files["database-password"]
	zitadelSecret := files["zitadel-client-secret"]
	portalSecret := files["portal-client-secret"]
	if settings["databaseName"] != "customer_dex" || !dexClientID.MatchString(settings["zitadelClientID"]) ||
		!materialPasswordValid(zitadelSecret) || !materialPasswordValid(portalSecret) ||
		bytes.Equal(zitadelSecret, portalSecret) || bytes.Equal(databaseSecret, zitadelSecret) || bytes.Equal(databaseSecret, portalSecret) {
		return nil, errMaterial
	}
	broker, err := endpoint(endpoints.DexIssuer, "")
	if err != nil || materialServiceTLS(broker, files) != nil {
		return nil, errMaterial
	}
	if _, err := materialRoots(files["zitadel-ca.pem"]); err != nil {
		return nil, errMaterial
	}
	config, err := json.MarshalIndent(nativeDexConfig(endpoints, dexCredentials{
		databaseHost: settings["databaseHost"], databaseUser: settings["databaseUser"], databasePassword: string(databaseSecret),
		zitadelClientID: settings["zitadelClientID"], zitadelSecret: string(zitadelSecret),
		portalSecret: string(portalSecret), zitadelCA: "/run/customer-dex/zitadel-ca.pem",
	}), "", "  ")
	if err != nil {
		return nil, errMaterial
	}
	outputs := map[string][]byte{"config.json": config}
	for _, name := range []string{"database-ca.pem", "tls.crt", "tls.key", "zitadel-ca.pem"} {
		outputs[name] = bytes.Clone(files[name])
	}
	return outputs, nil
}
