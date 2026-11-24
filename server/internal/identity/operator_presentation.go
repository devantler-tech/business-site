package identity

import (
	"net/http"

	"github.com/justinas/nosurf"
)

func (b *browser) renderOperator(w http.ResponseWriter, r *http.Request, client string) {
	page := b.operatorData(r, "operator", client)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = b.templates().ExecuteTemplate(w, "operator.html", page)
}

// operatorData is presentation after the caller has established operator
// authority. Existing operator handlers keep their final authorization checks.
func (b *browser) operatorData(r *http.Request, kind, client string) pageData {
	d := b.data(r, kind, client, nosurf.Token(r))
	d.PreviewAvailable = kind == "operator" && b.foundation != nil && b.foundation.operatorOnly
	d.Intro = ""
	if kind != "operator" {
		d.TriageEnabled = b.foundation.triageReady(r.Context())
	}
	switch kind {
	case "operator-customers":
		d.Title, d.Intro = "Customers and workspaces", "Manage invitations and decide who has access to each workspace."
		if d.Lang == "da" {
			d.Title, d.Intro = "Kunder og arbejdsområder", "Administrer invitationer og adgang til hvert arbejdsområde."
		}
	case "operator-invite":
		d.Title = "Invite a customer"
		if d.Lang == "da" {
			d.Title = "Inviter en kunde"
		}
	case "operator-audit":
		d.Title, d.Intro = "Audit log", "See recent actions by operators and agents."
		if d.Lang == "da" {
			d.Title, d.Intro = "Revisionslog", "Se de seneste handlinger fra operatører og agenter."
		}
	case "operator-support":
		d.Title = "Read-only support view"
		if d.Lang == "da" {
			d.Title = "Supportvisning"
		}
	default:
		d.Title = "Operator workspace"
		if d.Lang == "da" {
			d.Title = "Operatørportal"
		}
	}
	return d
}
