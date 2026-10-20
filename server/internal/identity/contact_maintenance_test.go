package identity

import (
	"errors"
	"flag"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// This test-only option selects ADR 0014's identical source task, not an app flag.
var maintenanceTask = flag.Bool("contact-maintenance-task", false, "check the frozen bilingual source task instead of the unchanged presentation")

// TestContactMaintenanceInventory requires the real frame in every maintained view.
func TestContactMaintenanceInventory(t *testing.T) {
	for name, want := range map[string]int{"portal.html": 28, "contact.html": 12, "requests.html": 38, "triage.html": 30, "credits.html": 12} {
		t.Run(name, func(t *testing.T) {
			pages, err := renderedLinkPages(name)
			if err != nil || len(pages) != want {
				t.Fatalf("incomplete maintenance inventory: got=%d want=%d error=%v", len(pages), want, err)
			}
			for i, page := range pages {
				lang := "en"
				if i >= want/2 {
					lang = "da"
				}
				if !strings.HasSuffix(page.uri, "lang="+lang) {
					t.Fatal("inventory lost a language")
				}
				if err := checkMaintenanceDocument(page.markup, lang, name == "contact.html", *maintenanceTask); err != nil {
					t.Fatalf("view=%d language=%s: %v", i, lang, err)
				}
				if name == "contact.html" {
					view := []string{"", "invalid", "conflict", "verification", "confirm", "confirmed"}[i%6]
					if err := checkMaintenanceState(page.markup, maintenancePage(lang, view)); err != nil {
						t.Fatalf("view=%s language=%s: %v", view, lang, err)
					}
				}
			}
			t.Logf("actual Go documents=%d", len(pages))
		})
	}
}

// maintenancePage derives fictional state expectations from the unchanged builder.
func maintenancePage(lang, view string) contactPageData {
	r := httptest.NewRequest("GET", "https://portal.example.invalid/portal/contact?lang="+lang, nil)
	d := contactData{Name: "Fictional client", Email: "fixture@example.invalid", Revision: 1, EmailVerified: view == "confirmed"}
	problem := view
	if view == "confirm" || view == "confirmed" {
		problem = ""
	}
	return (&browser{}).contactPage(r, d, problem, view == "confirm")
}

// TestContactMaintenanceStateRejections prevents normal output masking other states.
func TestContactMaintenanceStateRejections(t *testing.T) {
	pages, err := renderedLinkPages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	for _, view := range []string{"invalid", "conflict", "verification", "confirm", "confirmed"} {
		t.Run(view, func(t *testing.T) {
			if checkMaintenanceState(pages[0].markup, maintenancePage("en", view)) == nil {
				t.Fatal("normal body passed as a different state")
			}
		})
	}
	for name, bad := range map[string]string{
		"missing nonce":        strings.Replace(pages[0].markup, `name="csrf_token"`, `name="missing"`, 1),
		"missing revision":     strings.Replace(pages[0].markup, `name="revision"`, `name="missing"`, 1),
		"wrong language input": strings.Replace(pages[0].markup, `name="lang" value="en"`, `name="lang" value="da"`, 1),
		"missing submitter":    strings.Replace(pages[0].markup, `<button type="submit">`, `<button type="button">`, 1),
		"submit override":      strings.Replace(pages[0].markup, `<button type="submit">`, `<button type="submit" formaction="/portal/elsewhere">`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if checkMaintenanceState(bad, maintenancePage("en", "")) == nil {
				t.Fatal("incomplete native form was accepted")
			}
		})
	}
}

// TestContactMaintenanceConfirmationRejections preserves the second native operation.
func TestContactMaintenanceConfirmationRejections(t *testing.T) {
	pages, err := renderedLinkPages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	markup := pages[4].markup
	start := strings.Index(markup, `<form method="post" action="/portal/contact/verify">`)
	if start < 0 {
		t.Fatal("actual confirmation form missing")
	}
	end := strings.Index(markup[start:], `</form>`)
	if end < 0 {
		t.Fatal("actual confirmation form incomplete")
	}
	form := markup[start : start+end+len(`</form>`)]
	for name, replacement := range map[string]string{
		"empty form":        `<form action="/portal/contact/verify"></form>`,
		"wrong method":      strings.Replace(form, `method="post"`, `method="get"`, 1),
		"missing nonce":     strings.Replace(form, `name="csrf_token"`, `name="missing"`, 1),
		"wrong revision":    strings.Replace(form, `name="revision" value="1"`, `name="revision" value="2"`, 1),
		"missing help":      strings.Replace(form, `id="contact-verification-help"`, `id="absent"`, 1),
		"disconnected help": strings.Replace(form, `aria-describedby="contact-verification-help"`, `aria-describedby="absent"`, 1),
		"missing submitter": strings.Replace(form, `type="submit"`, `type="button"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if checkMaintenanceState(strings.Replace(markup, form, replacement, 1), maintenancePage("en", "confirm")) == nil {
				t.Fatal("incomplete confirmation form was accepted")
			}
		})
	}
}

// TestContactMaintenanceFormEnvelopeRejections challenges both forms' submission boundary.
func TestContactMaintenanceFormEnvelopeRejections(t *testing.T) {
	pages, err := renderedLinkPages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	for _, view := range []struct {
		index          int
		problem, start string
	}{
		{0, "", `<form class="contact-form"`},
		{4, "confirm", `<form method="post" action="/portal/contact/verify"`},
	} {
		markup := pages[view.index].markup
		start := strings.Index(markup, view.start)
		if start < 0 {
			t.Fatal("actual native form missing")
		}
		end := strings.Index(markup[start:], `</form>`)
		if end < 0 {
			t.Fatal("actual native form incomplete")
		}
		form := markup[start : start+end+len(`</form>`)]
		for name, replacement := range map[string]string{
			"textarea":          strings.Replace(form, `</form>`, `<textarea name="extra"></textarea></form>`, 1),
			"select":            strings.Replace(form, `</form>`, `<select name="extra"><option>extra</option></select></form>`, 1),
			"output":            strings.Replace(form, `</form>`, `<output name="extra"></output></form>`, 1),
			"object":            strings.Replace(form, `</form>`, `<object name="extra"></object></form>`, 1),
			"disabled fieldset": strings.Replace(strings.Replace(form, `>`, `><fieldset disabled>`, 1), `</form>`, `</fieldset></form>`, 1),
			"disabled ancestor": `<fieldset disabled>` + form + `</fieldset>`,
			"fieldset owner":    strings.Replace(form, `</form>`, `<fieldset form="foreign"></fieldset></form>`, 1),
			"submit encoding":   strings.Replace(form, `<button `, `<button formenctype="text/plain" `, 1),
			"submit target":     strings.Replace(form, `<button `, `<button formtarget="_blank" `, 1),
			"submit validation": strings.Replace(form, `<button `, `<button formnovalidate `, 1),
			"form encoding":     strings.Replace(form, `<form `, `<form enctype="text/plain" `, 1),
			"form target":       strings.Replace(form, `<form `, `<form target="_blank" `, 1),
			"form validation":   strings.Replace(form, `<form `, `<form novalidate `, 1),
		} {
			t.Run(strconv.Itoa(view.index)+"/"+name, func(t *testing.T) {
				if replacement == form {
					t.Fatal("refusal control did not alter its fixture")
				}
				if checkMaintenanceState(strings.Replace(markup, form, replacement, 1), maintenancePage("en", view.problem)) == nil {
					t.Fatal("changed native form envelope was accepted")
				}
			})
		}
	}
}

// checkMaintenanceState compares actual output with the expected native operations.
func checkMaintenanceState(markup string, page contactPageData) error {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return errors.New("cannot parse contact state")
	}
	alerts, confirmations, pending := 0, 0, 0
	var form, confirmation *html.Node
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if linkAttribute(n, "role") == "alert" {
			alerts++
			if page.FormError == "" || maintenanceText(n) != page.FormError {
				return errors.New("contact error state changed")
			}
		}
		if n.Data == "form" && linkAttribute(n, "action") == "/portal/contact/verify" {
			confirmations++
			confirmation = n
		}
		if linkAttribute(n, "id") == "contact-pending" {
			pending++
			if maintenanceText(n) != page.Pending {
				return errors.New("contact verification display changed")
			}
		}
		if n.Data == "form" && linkAttribute(n, "class") == "contact-form" {
			if form != nil {
				return errors.New("duplicate native contact form")
			}
			form = n
		}
	}
	wantAlerts, wantConfirmations := 0, 0
	if page.FormError != "" {
		wantAlerts = 1
	}
	if page.VerificationEnabled {
		wantConfirmations = 1
	}
	if alerts != wantAlerts || confirmations != wantConfirmations || pending != 1 || form == nil {
		return errors.New("contact state is incomplete or collapsed")
	}
	inputs := map[string]string{"csrf_token": page.CSRF, "lang": page.Lang, "revision": strconv.FormatInt(page.Contact.Revision, 10), "name": page.Contact.Name, "email": page.Contact.Email, "company": page.Contact.Company}
	if err := checkMaintenanceForm(form, "/portal/contact", inputs, page.Save, ""); err != nil {
		return err
	}
	if confirmation != nil {
		inputs = map[string]string{"csrf_token": page.CSRF, "lang": page.Lang, "revision": strconv.FormatInt(page.Contact.Revision, 10)}
		if err := checkMaintenanceForm(confirmation, "/portal/contact/verify", inputs, page.Verify, "contact-verification-help"); err != nil {
			return err
		}
		help := 0
		for n := range doc.Descendants() {
			if n.Type == html.ElementNode && linkAttribute(n, "id") == "contact-verification-help" {
				help++
				if maintenanceText(n) != page.VerificationHelp {
					return errors.New("confirmation help changed")
				}
			}
		}
		if help != 1 {
			return errors.New("confirmation help missing or duplicated")
		}
	}
	return nil
}

// Both native forms use the same closed input/submit contract; counting a form's
// action alone cannot establish that it still supports the requested operation.
func checkMaintenanceForm(form *html.Node, action string, wantInputs map[string]string, label, help string) error {
	if linkAttribute(form, "method") != "post" || linkAttribute(form, "action") != action {
		return errors.New("native form operation changed")
	}
	for _, attr := range form.Attr {
		switch attr.Key {
		case "enctype", "target", "novalidate":
			return errors.New("native form submission behavior changed")
		}
	}
	for parent := form.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == "fieldset" {
			for _, attr := range parent.Attr {
				if attr.Key == "disabled" {
					return errors.New("native form inherited a disabled fieldset")
				}
			}
		}
	}
	counts := map[string]int{}
	submitters := 0
	for n := range form.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		switch n.Data {
		case "textarea", "select", "output", "object":
			return errors.New("native form gained an unexpected control")
		case "fieldset":
			for _, attr := range n.Attr {
				if attr.Key == "disabled" || attr.Key == "form" {
					return errors.New("native form control association or behavior changed")
				}
			}
		}
		if n.Data == "input" || n.Data == "button" {
			for _, attr := range n.Attr {
				switch attr.Key {
				case "form", "formaction", "formmethod", "formenctype", "formtarget", "formnovalidate", "disabled", "readonly":
					return errors.New("native form control association or behavior changed")
				}
			}
		}
		if n.Data == "input" {
			name := linkAttribute(n, "name")
			value, known := wantInputs[name]
			if !known || linkAttribute(n, "value") != value {
				return errors.New("native contact input value changed")
			}
			if (name == "csrf_token" || name == "lang" || name == "revision") && linkAttribute(n, "type") != "hidden" {
				return errors.New("native hidden control changed")
			}
			counts[name]++
		}
		if n.Data == "button" {
			if (linkAttribute(n, "type") != "" && linkAttribute(n, "type") != "submit") || maintenanceText(n) != label || linkAttribute(n, "aria-describedby") != help {
				return errors.New("native submit behavior, label or help changed")
			}
			submitters++
		}
	}
	for name := range wantInputs {
		if counts[name] != 1 {
			return errors.New("native input missing or duplicated")
		}
	}
	if submitters != 1 {
		return errors.New("native submitter missing or duplicated")
	}
	return nil
}

// TestContactMaintenanceRejections tests the validator, never comparative samples.
func TestContactMaintenanceRejections(t *testing.T) {
	pages, err := renderedLinkPages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	// A reviewed task fixture for the validator, not comparative evidence. The
	// real trial must edit source, compile it and render it separately.
	markup := pages[0].markup
	if !*maintenanceTask {
		markup = strings.Replace(markup, "Appearance", "Colour theme", 1)
		markup = strings.Replace(markup, `maxlength="120"`, `maxlength="120" aria-describedby="contact-name-help"`, 1)
		markup = strings.Replace(markup, `<label for="contact-email">`, `<p id="contact-name-help">You can update these details later.</p><label for="contact-email">`, 1)
	}
	if err := checkMaintenanceDocument(markup, "en", true, true); err != nil {
		t.Fatal("complete validator fixture was rejected", err)
	}
	for name, bad := range map[string]string{
		"old theme":           strings.Replace(markup, "Colour theme", "Appearance", 1),
		"missing hint":        strings.Replace(markup, `<p id="contact-name-help">You can update these details later.</p>`, "", 1),
		"duplicate hint":      strings.Replace(markup, `</main>`, `<p id="contact-name-help">You can update these details later.</p></main>`, 1),
		"disconnected hint":   strings.Replace(markup, `aria-describedby="contact-name-help"`, `aria-describedby="absent"`, 1),
		"wrong hint":          strings.Replace(markup, "You can update these details later.", "Different hint", 1),
		"lost name limit":     strings.Replace(markup, `maxlength="120"`, `maxlength="121"`, 1),
		"lost email help":     strings.Replace(markup, `aria-describedby="contact-pending"`, "", 1),
		"lost required":       strings.Replace(markup, ` required`, "", 1),
		"wrong native action": strings.Replace(markup, `action="/portal/contact"`, `action="/portal/elsewhere"`, 1),
		"missing document":    "<p>Partial output</p>",
		"wrong language":      strings.Replace(markup, `lang="en"`, `lang="da"`, 1),
		"duplicate field":     strings.Replace(markup, `</main>`, `<input id="contact-name"></main>`, 1),
		"lost email limit":    strings.Replace(markup, `maxlength="254"`, `maxlength="255"`, 1),
		"lost company limit":  strings.Replace(markup, `maxlength="160"`, `maxlength="161"`, 1),
		"hidden name":         strings.Replace(markup, `name="name"`, `name="name" type="hidden"`, 1),
		"checkbox name":       strings.Replace(markup, `name="name"`, `name="name" type="checkbox"`, 1),
		"required company":    strings.Replace(markup, `name="company"`, `name="company" required`, 1),
		"checkbox company":    strings.Replace(markup, `name="company"`, `name="company" type="checkbox"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if bad == markup {
				t.Fatal("refusal control did not alter its fixture")
			}
			if err := checkMaintenanceDocument(bad, "en", true, true); err == nil {
				t.Fatal("incomplete or altered maintenance output was accepted")
			}
		})
	}
}

// checkMaintenanceDocument checks the real private frame separately from its body.
func checkMaintenanceDocument(markup, lang string, contact, task bool) error {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return errors.New("cannot parse presentation")
	}
	counts := map[string]int{}
	theme := "Appearance"
	if task {
		theme = "Colour theme"
	}
	if lang == "da" {
		theme = "Udseende"
		if task {
			theme = "Farvetema"
		}
	}
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if n.Data == "html" && linkAttribute(n, "lang") == lang {
			counts["language"]++
		}
		if n.Data == "main" && linkAttribute(n, "id") == "main" {
			counts["main"]++
		}
		if n.Data == "footer" && maintenanceText(n) == "Devantler Tech" {
			counts["footer"]++
		}
		if n.Data == "meta" && linkAttribute(n, "name") == "robots" && linkAttribute(n, "content") == "noindex,nofollow" {
			counts["private"]++
		}
		if n.Data == "select" && linkAttribute(n, "id") == "theme" {
			if n.Parent == nil || n.Parent.Data != "label" || maintenanceText(n.Parent) != theme {
				return errors.New("appearance label is not associated with its control")
			}
			counts["theme"]++
		}
	}
	for _, key := range []string{"language", "main", "footer", "private", "theme"} {
		if counts[key] != 1 {
			return errors.New("incomplete or duplicated private frame")
		}
	}
	if contact {
		return checkMaintenanceContact(markup, lang, task)
	}
	return nil
}

// Select options have their own labels, not part of the wrapping label's name.
func maintenanceText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.Data != "select" {
			text.WriteString(maintenanceText(child))
		}
	}
	return strings.TrimSpace(text.String())
}

// checkMaintenanceContact preserves field semantics and the task's accessible hint.
func checkMaintenanceContact(markup, lang string, task bool) error {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return errors.New("cannot parse contact body")
	}
	ids := map[string][]*html.Node{}
	var forms []*html.Node
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if id := linkAttribute(n, "id"); id != "" {
			ids[id] = append(ids[id], n)
		}
		if n.Data == "form" && linkAttribute(n, "class") == "contact-form" {
			forms = append(forms, n)
		}
	}
	if len(forms) != 1 || linkAttribute(forms[0], "method") != "post" || linkAttribute(forms[0], "action") != "/portal/contact" {
		return errors.New("native contact form changed")
	}
	for _, field := range []struct{ id, name, limit, autocomplete string }{
		{"contact-name", "name", "120", "name"},
		{"contact-email", "email", "254", "email"},
		{"contact-company", "company", "160", "organization"},
	} {
		if len(ids[field.id]) != 1 {
			return errors.New("missing or duplicate contact field")
		}
		n := ids[field.id][0]
		if n.Data != "input" || linkAttribute(n, "name") != field.name || linkAttribute(n, "maxlength") != field.limit || linkAttribute(n, "autocomplete") != field.autocomplete {
			return errors.New("contact input contract changed")
		}
		kind := linkAttribute(n, "type")
		if (field.name == "email" && kind != "email") || (field.name != "email" && kind != "" && kind != "text") {
			return errors.New("contact field type changed")
		}
		parent := n.Parent
		for parent != nil && parent != forms[0] {
			parent = parent.Parent
		}
		if parent == nil {
			return errors.New("contact input is outside its native form")
		}
		required := false
		for _, attr := range n.Attr {
			required = required || attr.Key == "required"
		}
		if required != (field.name != "company") {
			return errors.New("contact field requirement changed")
		}
	}
	if linkAttribute(ids["contact-email"][0], "type") != "email" || linkAttribute(ids["contact-email"][0], "aria-describedby") != "contact-pending" || len(ids["contact-pending"]) != 1 || maintenanceText(ids["contact-pending"][0]) == "" {
		return errors.New("email help or semantics changed")
	}
	if task {
		hint := "You can update these details later."
		if lang == "da" {
			hint = "Du kan ændre oplysningerne senere."
		}
		if linkAttribute(ids["contact-name"][0], "aria-describedby") != "contact-name-help" || len(ids["contact-name-help"]) != 1 || maintenanceText(ids["contact-name-help"][0]) != hint {
			return errors.New("name help is missing, ambiguous or disconnected")
		}
	}
	return nil
}
