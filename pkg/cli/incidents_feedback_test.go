package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// recordingServer serves routes and records each request's path, query and
// JSON body.
type recorded struct {
	method, path, query string
	body                map[string]any
}

func recordingServer(t *testing.T, routes map[string]any) (*httptest.Server, func() []recorded) {
	t.Helper()
	var mu sync.Mutex
	var reqs []recorded
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		reqs = append(reqs, recorded{r.Method, r.URL.Path, r.URL.RawQuery, b})
		mu.Unlock()
		resp, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			resp = map[string]any{"id": "ok"}
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated) // the SDK rejects a 200 on create
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ts.Close)
	return ts, func() []recorded {
		mu.Lock()
		defer mu.Unlock()
		return append([]recorded(nil), reqs...)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Incident #670: `incidents chat --body @file` posted the literal path.
func TestCuratedTextFlagsReadFiles(t *testing.T) {
	path := writeTemp(t, noteText)
	for _, tc := range []struct {
		args  []string
		field string
	}{
		{[]string{"incidents", "note", "inc-1", "--body", "@" + path}, "body"},
		{[]string{"incidents", "chat", "inc-1", "--body", "@" + path}, "body"},
		{[]string{"incidents", "update", "inc-1", "--description", "@" + path}, "description"},
		{[]string{"incidents", "update", "inc-1", "--summary", "@" + path}, "summary"},
	} {
		t.Run(strings.Join(tc.args[:2], " ")+" "+tc.field, func(t *testing.T) {
			ts, reqs := recordingServer(t, nil)
			if _, stderr, err := runCmdFull(t, ts.URL, tc.args...); err != nil {
				t.Fatalf("%v %s", err, stderr)
			}
			got := reqs()
			if len(got) != 1 || got[0].body[tc.field] != noteText {
				t.Errorf("%s = %v, want the file's contents", tc.field, got)
			}
		})
	}
}

func TestCuratedTextFlagMissingFileIsAnError(t *testing.T) {
	ts, reqs := recordingServer(t, nil)

	_, stderr, err := runCmdFull(t, ts.URL, "incidents", "chat", "inc-1", "--body", "@/no/such/file.md")

	if err == nil || !strings.Contains(stderr, "/no/such/file.md") {
		t.Errorf("err = %v, stderr = %s; want an error naming the file", err, stderr)
	}
	if len(reqs()) != 0 {
		t.Error("nothing should be sent when the file is missing")
	}
}

func TestCuratedTextFlagReadsStdin(t *testing.T) {
	ts, reqs := recordingServer(t, nil)
	old := stdinReader
	stdinReader = strings.NewReader(noteText)
	t.Cleanup(func() { stdinReader = old })

	if _, stderr, err := runCmdFull(t, ts.URL, "incidents", "note", "inc-1", "--body", "@-"); err != nil {
		t.Fatalf("%v %s", err, stderr)
	}
	if got := reqs(); len(got) != 1 || got[0].body["body"] != noteText {
		t.Errorf("requests = %v, want the note read from stdin", got)
	}
}

// Incident #671: the skill says to pass --visibility, but the curated note
// command didn't define it.
func TestIncidentsNoteVisibility(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "private_to_org"},
		{[]string{"--visibility", "open_to_public"}, "open_to_public"},
	} {
		ts, reqs := recordingServer(t, nil)
		args := append([]string{"incidents", "note", "inc-1", "--body", "x"}, tc.args...)
		if _, stderr, err := runCmdFull(t, ts.URL, args...); err != nil {
			t.Fatalf("%v %s", err, stderr)
		}
		if got := reqs(); len(got) != 1 || got[0].body["visibility"] != tc.want {
			t.Errorf("%v: requests = %v, want visibility %s", tc.args, got, tc.want)
		}
	}
}

func TestIncidentsNoteRejectsUnknownVisibility(t *testing.T) {
	ts, reqs := recordingServer(t, nil)

	_, stderr, err := runCmdFull(t, ts.URL, "incidents", "note", "inc-1", "--body", "x", "--visibility", "public")

	if err == nil || !strings.Contains(stderr, "open_to_public") {
		t.Errorf("err = %v, stderr = %s; want an error listing the values", err, stderr)
	}
	if len(reqs()) != 0 {
		t.Error("nothing should be sent")
	}
}

// Incident #671: --types chat_message silently returned zero events, because
// the API only matches event/chat_message.
func TestIncidentsEventsNormalizesTypes(t *testing.T) {
	ts, reqs := recordingServer(t, map[string]any{"GET /v1/incidents/inc-1/events": map[string]any{"data": []any{}}})

	if _, stderr, err := runCmdFull(t, ts.URL, "incidents", "events", "inc-1", "--types", "chat_message, event/note"); err != nil {
		t.Fatalf("%v %s", err, stderr)
	}
	got := reqs()
	if len(got) != 1 || !strings.Contains(got[0].query, "types=event%2Fchat_message%2Cevent%2Fnote") {
		t.Errorf("requests = %v, want types=event/chat_message,event/note", got)
	}
}

func TestIncidentsEventsRejectsUnknownType(t *testing.T) {
	ts, reqs := recordingServer(t, nil)

	_, stderr, err := runCmdFull(t, ts.URL, "incidents", "events", "inc-1", "--types", "chat")

	if err == nil || !strings.Contains(stderr, "chat_message") {
		t.Errorf("err = %v, stderr = %s; want an error listing the valid types", err, stderr)
	}
	if len(reqs()) != 0 {
		t.Error("nothing should be sent")
	}
}

var attachedPages = map[string]any{"data": []any{
	map[string]any{
		"id": "isp-public", "name": "FireHydrant", "display_name": "FireHydrant Status", "url": "https://status.firehydrant.com",
		"integration": map[string]any{"id": "conn-public", "integration_slug": "nunc"},
	},
	map[string]any{
		"id": "isp-other", "name": "Customer page", "url": "https://status.example.com",
		"integration": map[string]any{"id": "conn-other", "integration_slug": "nunc"},
	},
}}

func statusPagesServer(t *testing.T) (*httptest.Server, func() []recorded) {
	return recordingServer(t, map[string]any{
		"GET /v1/incidents/inc-1/status_pages": attachedPages,
		"PATCH /v1/incidents/inc-1/impact":     map[string]any{"id": "inc-1"},
	})
}

func patches(reqs []recorded) []recorded {
	var out []recorded
	for _, r := range reqs {
		if r.method == http.MethodPatch {
			out = append(out, r)
		}
	}
	return out
}

// Incident #671: a milestone + note update without status_pages reached only
// the internal page. status-update sends the pages' connection ids.
func TestIncidentsStatusUpdateTargetsPagesByConnection(t *testing.T) {
	ts, reqs := statusPagesServer(t)
	path := writeTemp(t, noteText)

	out, stderr, err := runCmdFull(t, ts.URL, "incidents", "status-update", "inc-1",
		"--note", "@"+path, "--milestone", "identified", "--page", "status.firehydrant.com")
	if err != nil {
		t.Fatalf("%v %s", err, stderr)
	}

	p := patches(reqs())
	if len(p) != 1 || p[0].path != "/v1/incidents/inc-1/impact" {
		t.Fatalf("patches = %v", p)
	}
	b := p[0].body
	if b["note"] != noteText || b["milestone"] != "identified" {
		t.Errorf("body = %v", b)
	}
	pages, _ := b["status_pages"].([]any)
	if len(pages) != 1 {
		t.Fatalf("status_pages = %v, want one", b["status_pages"])
	}
	page := pages[0].(map[string]any)
	if page["id"] != "conn-public" || page["integration_slug"] != "nunc" {
		t.Errorf("status_pages[0] = %v, want the connection id, not the incident status page id", page)
	}
	if !strings.Contains(out, `"published_to":["FireHydrant Status (https://status.firehydrant.com)"]`) {
		t.Errorf("output should say where the update went: %s", out)
	}
}

func TestIncidentsStatusUpdateAllPagesAndNoEmail(t *testing.T) {
	ts, reqs := statusPagesServer(t)

	if _, stderr, err := runCmdFull(t, ts.URL, "incidents", "status-update", "inc-1", "--note", "x", "--all-pages", "--no-email"); err != nil {
		t.Fatalf("%v %s", err, stderr)
	}

	b := patches(reqs())[0].body
	if pages, _ := b["status_pages"].([]any); len(pages) != 2 {
		t.Errorf("status_pages = %v, want both attached pages", b["status_pages"])
	}
	if b["notify_email_subscribers"] != false {
		t.Errorf("notify_email_subscribers = %v, want false", b["notify_email_subscribers"])
	}
}

func TestIncidentsStatusUpdateInternalOnly(t *testing.T) {
	ts, reqs := statusPagesServer(t)

	out, stderr, err := runCmdFull(t, ts.URL, "incidents", "status-update", "inc-1", "--note", "x", "--internal-only")
	if err != nil {
		t.Fatalf("%v %s", err, stderr)
	}

	if _, ok := patches(reqs())[0].body["status_pages"]; ok {
		t.Error("--internal-only should send no status_pages")
	}
	if !strings.Contains(out, "internal status page") {
		t.Errorf("output = %s", out)
	}
}

func TestIncidentsStatusUpdateRequiresATarget(t *testing.T) {
	for _, args := range [][]string{
		{},                                   // none: the #671 trap
		{"--all-pages", "--internal-only"},   // conflicting
		{"--page", "status.nowhere.example"}, // not attached
	} {
		ts, reqs := statusPagesServer(t)
		full := append([]string{"incidents", "status-update", "inc-1", "--note", "x"}, args...)

		_, stderr, err := runCmdFull(t, ts.URL, full...)

		if err == nil || !strings.Contains(stderr, "status.firehydrant.com") {
			t.Errorf("%v: err = %v, stderr = %s; want an error listing the attached pages", args, err, stderr)
		}
		if len(patches(reqs())) != 0 {
			t.Errorf("%v: nothing should be sent", args)
		}
	}
}

func TestIncidentsStatusUpdateDryRun(t *testing.T) {
	ts, reqs := statusPagesServer(t)

	out, stderr, err := runCmdFull(t, ts.URL, "incidents", "status-update", "inc-1", "--note", "x", "--page", "Customer page", "--dry-run")
	if err != nil {
		t.Fatalf("%v %s", err, stderr)
	}

	if len(patches(reqs())) != 0 {
		t.Error("--dry-run must not send the update")
	}
	for _, want := range []string{`"method":"PATCH"`, `"conn-other"`, "publishes to: Customer page (https://status.example.com)"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q: %s", want, out)
		}
	}
}
