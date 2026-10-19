package identity

import (
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type journeySample struct {
	Actor, Phase  string
	Status, Bytes int
	ElapsedNS     int64
}

type journeyRun struct {
	SignInNS  []int64
	Responses []journeySample
}

type journeyReport struct {
	Schema, Go, OS, Arch string
	Warmups, Journeys    int
	Runs                 []journeyRun
}

// completeJourneyReport admits only complete, ordered observations with fixed
// public labels; incomplete runs cannot earn a baseline or leak response data.
func completeJourneyReport(runs []journeyRun, expected int) (journeyReport, error) {
	if expected <= 0 || len(runs) != expected {
		return journeyReport{}, errors.New("incomplete journey count")
	}
	for _, run := range runs {
		if len(run.SignInNS) != 3 || len(run.Responses) != 21 {
			return journeyReport{}, errors.New("incomplete journey observations")
		}
		for _, elapsed := range run.SignInNS {
			if elapsed <= 0 {
				return journeyReport{}, errors.New("missing sign-in timing")
			}
		}
		n := 0
		for _, actor := range []string{"client-a", "client-b", "operator"} {
			phases := []string{"workspace", "contact", "save", "reload", "stale-edit", "recover", "logout", "signed-out"}
			statuses := []int{200, 200, 303, 200, 409, 200, 303, 401}
			if actor == "operator" {
				phases = []string{"workspace", "operator", "contact-denied", "logout", "signed-out"}
				statuses = []int{200, 200, 403, 303, 403}
			}
			for i, phase := range phases {
				sample := run.Responses[n]
				if sample.Actor != actor || sample.Phase != phase || sample.Status != statuses[i] || sample.Bytes < 0 || sample.ElapsedNS <= 0 {
					return journeyReport{}, errors.New("invalid journey observation")
				}
				n++
			}
		}
	}
	return journeyReport{"private-journey/v1", runtime.Version(), runtime.GOOS, runtime.GOARCH, 20, len(runs), runs}, nil
}

type journeyActor struct {
	id, lang, name, email, invitation string
	client                            *http.Client
}

// journeyActors gives each fictional identity its own invitation and cookie jar
// while retaining one shared application and disposable database.
func journeyActors(f *browserFixture) []journeyActor {
	f.t.Helper()
	actors := []journeyActor{{id: "client-a", lang: "en", name: "Alice Fictional", email: "alice@example.invalid"}, {id: "client-b", lang: "da", name: "Bob Fictional", email: "bob@example.invalid"}, {id: "operator", lang: "da"}}
	for i := range actors {
		a := &actors[i]
		a.client = f.site.Client()
		a.client.Jar, _ = cookiejar.New(nil)
		a.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		role := "client"
		if a.id == "operator" {
			role = "operator"
		}
		a.invitation = f.invite(a.id, a.id, role)
	}
	return actors
}

var contactRevisionInput = regexp.MustCompile(`name="revision" value="([0-9]+)"`)

// journeyForm reads the actual native form's CSRF token and durable revision;
// the measured journey must not manufacture either value.
func journeyForm(f *browserFixture, body string) (string, int64) {
	f.t.Helper()
	token := csrfInput.FindStringSubmatch(body)
	revision := contactRevisionInput.FindStringSubmatch(body)
	if len(token) != 2 || len(revision) != 2 {
		f.t.Fatal("journey contact form lacks actual CSRF/revision fields")
	}
	n, err := strconv.ParseInt(revision[1], 10, 64)
	if err != nil {
		f.t.Fatal("journey contact revision is invalid")
	}
	return html.UnescapeString(token[1]), n
}

// privateContactJourney exercises real sign-in, authorized persistence, stale-edit
// recovery and logout, retaining only bounded metadata for each response phase.
func privateContactJourney(f *browserFixture, actors []journeyActor) journeyRun {
	f.t.Helper()
	run := journeyRun{}
	for i := range actors {
		a := &actors[i]
		f.client = a.client
		f.mu.Lock()
		f.subject = a.id
		f.mu.Unlock()
		started := time.Now()
		f.finish(f.start(a.invitation))
		run.SignInNS = append(run.SignInNS, time.Since(started).Nanoseconds())
		a.invitation = "" // Existing membership signs in without re-enrollment.
		request := func(phase, method, path string, form url.Values, want int) (string, http.Header) {
			f.t.Helper()
			origin := ""
			if method == "POST" {
				origin = f.site.URL
			}
			started := time.Now()
			status, body, headers := f.request(method, path, form, origin)
			elapsed := time.Since(started).Nanoseconds()
			if status != want || headers.Get("Cache-Control") != "no-store" {
				f.t.Fatalf("journey %s/%s status=%d want=%d or missing private cache policy", a.id, phase, status, want)
			}
			run.Responses = append(run.Responses, journeySample{a.id, phase, status, len(body), elapsed})
			return body, headers
		}
		workspace, _ := request("workspace", "GET", "/portal/?lang="+a.lang, nil, 200)
		if !strings.Contains(workspace, a.id) || !strings.Contains(workspace, `lang="`+a.lang+`"`) {
			f.t.Fatal("journey lost the server-owned workspace or selected language")
		}
		if a.id == "operator" {
			request("operator", "GET", "/portal/operator?lang=da", nil, 200)
			request("contact-denied", "GET", "/portal/contact?lang=da", nil, 403)
			token := csrfInput.FindStringSubmatch(workspace)
			if len(token) != 2 {
				f.t.Fatal("journey operator logout token missing")
			}
			request("logout", "POST", "/portal/logout", url.Values{"csrf_token": {html.UnescapeString(token[1])}, "lang": {a.lang}}, 303)
			request("signed-out", "GET", "/portal/operator", nil, 403)
			continue
		}
		body, _ := request("contact", "GET", "/portal/contact?lang="+a.lang, nil, 200)
		token, revision := journeyForm(f, body)
		form := contactForm(token, strconv.FormatInt(revision, 10), a.name, a.email, "")
		form.Set("lang", a.lang)
		_, headers := request("save", "POST", "/portal/contact", form, 303)
		if headers.Get("Location") != "/portal/contact?lang="+a.lang {
			f.t.Fatal("journey save left its local contact route")
		}
		body, _ = request("reload", "GET", "/portal/contact?lang="+a.lang, nil, 200)
		_, savedRevision := journeyForm(f, body)
		if savedRevision != revision+1 || !strings.Contains(body, `value="`+a.name+`"`) || !strings.Contains(body, `value="`+a.email+`"`) {
			f.t.Fatal("journey reload lost the saved contact revision or values")
		}
		other := "Alice Fictional"
		if a.id == "client-a" {
			other = "Bob Fictional"
		}
		if strings.Contains(body, other) {
			f.t.Fatal("journey exposed the other fictional client's contact")
		}
		form.Set("name", "Rejected stale edit")
		body, _ = request("stale-edit", "POST", "/portal/contact", form, 409)
		if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `lang="`+a.lang+`"`) {
			f.t.Fatal("journey stale edit lost localized recoverable feedback")
		}
		body, _ = request("recover", "GET", "/portal/contact?lang="+a.lang, nil, 200)
		token, recoveredRevision := journeyForm(f, body)
		if recoveredRevision != savedRevision || !strings.Contains(body, `value="`+a.name+`"`) || !strings.Contains(body, `value="`+a.email+`"`) || strings.Contains(body, "Rejected stale edit") {
			f.t.Fatal("journey stale edit changed the saved record")
		}
		request("logout", "POST", "/portal/logout", url.Values{"csrf_token": {token}, "lang": {a.lang}}, 303)
		request("signed-out", "GET", "/portal/contact?lang="+a.lang, nil, 401)
	}
	return run
}

// A benchmark that omits the actual write/reload or logout must not earn a
// complete measurement merely because its HTTP requests were fast.
func TestPrivateJourneyPersistsEachClientAndLogsOut(t *testing.T) {
	f := newBrowserFixture(t)
	actors := journeyActors(f)
	run := privateContactJourney(f, actors)
	if len(run.Responses) != 21 || len(run.SignInNS) != 3 {
		t.Fatalf("complete journey observations=%d want 21, sign-ins=%d want 3", len(run.Responses), len(run.SignInNS))
	}
	rows, err := f.pool.Query(f.f.ctx, "SELECT client_id,name,email,revision FROM portal_contacts ORDER BY client_id")
	if err != nil {
		t.Fatal("read fictional contact outcomes")
	}
	defer rows.Close()
	for _, want := range []struct{ id, name, email string }{{"client-a", "Alice Fictional", "alice@example.invalid"}, {"client-b", "Bob Fictional", "bob@example.invalid"}} {
		var id, name, email string
		var revision int64
		if !rows.Next() || rows.Scan(&id, &name, &email, &revision) != nil || id != want.id || name != want.name || email != want.email || revision != 1 {
			t.Fatal("journey did not preserve separate expected client records")
		}
	}
	if rows.Next() || rows.Err() != nil {
		t.Fatal("unexpected or incomplete contact outcomes")
	}
	rows.Close()
	var authenticated int
	if err := f.pool.QueryRow(f.f.ctx, "SELECT count(*) FROM portal_sessions").Scan(&authenticated); err != nil || authenticated != 0 {
		t.Fatal("journey left a durable session after logout")
	}
	privateContactJourney(f, actors)
	var updated int
	if err := f.pool.QueryRow(f.f.ctx, "SELECT count(*) FROM portal_contacts WHERE revision=2 AND name!='Rejected stale edit'").Scan(&updated); err != nil || updated != 2 {
		t.Fatal("repeated journey did not continue the same two saved records")
	}
}

// A plausible partial run, wrong response, or contaminated label must not be
// serialized as a complete baseline, even when another run succeeded.
func TestPrivateJourneyReportRejectsIncompleteEvidence(t *testing.T) {
	f := newBrowserFixture(t)
	valid := privateContactJourney(f, journeyActors(f))
	for _, kind := range []string{"missing trial", "missing response", "duplicate response", "wrong status", "missing timing", "negative bytes", "unsafe label", "missing sign-in"} {
		t.Run(kind, func(t *testing.T) {
			run := journeyRun{SignInNS: append([]int64(nil), valid.SignInNS...), Responses: append([]journeySample(nil), valid.Responses...)}
			runs := []journeyRun{valid, run}
			switch kind {
			case "missing trial":
				runs = runs[:1]
			case "missing response":
				runs[1].Responses = run.Responses[:20]
			case "duplicate response":
				run.Responses[1] = run.Responses[0]
			case "wrong status":
				run.Responses[0].Status = 503
			case "missing timing":
				run.Responses[0].ElapsedNS = 0
			case "negative bytes":
				run.Responses[0].Bytes = -1
			case "unsafe label":
				run.Responses[0].Phase = "synthetic-sensitive-value"
			case "missing sign-in":
				runs[1].SignInNS = run.SignInNS[:2]
			}
			if _, err := completeJourneyReport(runs, 2); err == nil {
				t.Fatalf("accepted %s as a complete baseline", kind)
			}
		})
	}
	if _, err := completeJourneyReport([]journeyRun{valid}, 1); err != nil {
		t.Fatal("rejected the complete actual journey")
	}
}

// BenchmarkPrivateContactJourney is opt-in: ordinary tests exercise its actual
// outcomes once, while explicit benchmark runs retain complete raw observations.
// Fixture resets and warmups are outside b.Loop's timed interval.
func BenchmarkPrivateContactJourney(b *testing.B) {
	f := newBrowserFixture(b)
	actors := journeyActors(f)
	for range 20 {
		privateContactJourney(f, actors)
	}
	var runs []journeyRun
	for b.Loop() {
		runs = append(runs, privateContactJourney(f, actors))
	}
	report, err := completeJourneyReport(runs, b.N)
	if err != nil {
		b.Fatal("incomplete private journey measurement")
	}
	data, err := json.Marshal(report)
	if err != nil {
		b.Fatal("encode complete private journey measurement")
	}
	b.Log(string(data))
}
