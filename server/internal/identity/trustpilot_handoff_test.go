package identity

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing/fstest"

	"github.com/devantler-tech/business-site/server/internal/webapp"
)

// Check the runtime-owned handoff itself. The profile is opened by the
// visitor's browser after the runtime has served this response.
func checkTrustpilotHandoff() error {
	artifact, err := webapp.Load(fstest.MapFS{
		"index.html":    {Data: []byte("English fixture")},
		"da/index.html": {Data: []byte("Danish fixture")},
	})
	if err != nil {
		return err
	}
	handler := artifact.HostHandler(nil, webapp.Hosts{Public: "devantler.tech", Portal: "portal.devantler.tech"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "https://portal.devantler.tech/trustpilot/", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://www.trustpilot.com/review/devantler.tech" ||
		w.Header().Get("Referrer-Policy") != "no-referrer" {
		return errors.New("invalid Trustpilot profile handoff")
	}
	return nil
}
