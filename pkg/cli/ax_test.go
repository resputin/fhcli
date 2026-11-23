package cli

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Curated incident writes preview with --dry-run, like `fhcli api`.

func TestIncidentWritesDryRunSendNothing(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		method string
		path   string
		body   map[string]any
	}{
		{"note", []string{"incidents", "note", "inc-1", "--body", "found it"},
			"POST", "/v1/incidents/inc-1/notes", map[string]any{"body": "found it", "visibility": "private_to_org"}},
		{"note public", []string{"incidents", "note", "inc-1", "--body", "update", "--visibility", "open_to_public"},
			"POST", "/v1/incidents/inc-1/notes", map[string]any{"body": "update", "visibility": "open_to_public"}},
		{"chat", []string{"incidents", "chat", "#623", "--body", "checking"},
			"POST", "/v1/incidents/623/generic_chat_messages", map[string]any{"body": "checking"}},
		{"update", []string{"incidents", "update", "inc-1", "--severity", "SEV1", "--summary", "pool leak"},
			"PATCH", "/v1/incidents/inc-1", map[string]any{"severity": "SEV1", "summary": "pool leak"}},
		{"resolve", []string{"incidents", "resolve", "inc-1"},
			"PUT", "/v1/incidents/inc-1/resolve", nil},
		{"resolve with milestone", []string{"incidents", "resolve", "inc-1", "--milestone", "resolved"},
			"PUT", "/v1/incidents/inc-1/resolve", map[string]any{"milestone": "resolved"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := failIfCalled(t)

			out, _, err := runCmdFull(t, ts.URL, append(tc.args, "--dry-run")...)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			d := parseDryRun(t, out)
			if d.Method != tc.method || d.URL != ts.URL+tc.path {
				t.Errorf("request = %s %s, want %s %s", d.Method, d.URL, tc.method, ts.URL+tc.path)
			}
			if !reflect.DeepEqual(d.Body, tc.body) {
				t.Errorf("body = %v, want %v", d.Body, tc.body)
			}
		})
	}
}

func TestIncidentWriteDryRunNeedsNoCredentials(t *testing.T) {
	out, err := runCmdNoToken(t, "incidents", "note", "inc-1", "--body", "x", "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d := parseDryRun(t, out); d.Method != "POST" {
		t.Errorf("method = %q, want POST", d.Method)
	}
}

func TestIncidentWriteDryRunRequiresIncidentID(t *testing.T) {
	ts := failIfCalled(t)

	_, stderr, err := runCmdFull(t, ts.URL, "incidents", "resolve", "--dry-run")

	if err == nil || !strings.Contains(stderr, "incident ID is required") {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
}

func TestIncidentNoteVisibilityIsSentExplicitly(t *testing.T) {
	var got map[string]any
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"POST /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&got)
			jsonHandler(201, map[string]any{"id": "n1"})(w, r)
		},
	})
	defer ts.Close()

	_, err := runCmd(t, ts.URL, "incidents", "note", "inc-1", "--body", "x", "--visibility", "open_to_public")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["visibility"] != "open_to_public" {
		t.Errorf("visibility = %v, want open_to_public", got["visibility"])
	}
}

func TestIncidentNoteRejectsUnknownVisibility(t *testing.T) {
	ts := failIfCalled(t)

	_, stderr, err := runCmdFull(t, ts.URL, "incidents", "note", "inc-1", "--body", "x", "--visibility", "everyone")

	if err == nil || !strings.Contains(stderr, "private_to_org or open_to_public") {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
}

// `fhcli api` refuses to send a request that lacks required inputs.

type missingOutput struct {
	Error   string   `json:"error"`
	Missing []string `json:"missing"`
	Example string   `json:"example"`
}

func parseMissing(t *testing.T, stderr string) missingOutput {
	t.Helper()
	var m missingOutput
	if err := json.Unmarshal([]byte(stderr), &m); err != nil {
		t.Fatalf("stderr is not JSON: %v\n%s", err, stderr)
	}
	return m
}

func TestAPIRefusesToSendWithoutRequiredBodyField(t *testing.T) {
	ts := failIfCalled(t)

	_, stderr, err := runMini(t, ts.URL, "api", "incidents", "create-incident", "--priority", "2")

	if err == nil {
		t.Fatal("expected an error")
	}
	m := parseMissing(t, stderr)
	if !reflect.DeepEqual(m.Missing, []string{"--name"}) {
		t.Errorf("missing = %v, want [--name]", m.Missing)
	}
	if !strings.Contains(m.Example, "fhcli api incidents create-incident") || !strings.Contains(m.Example, "--name") {
		t.Errorf("example = %q", m.Example)
	}
}

func TestAPIDryRunAlsoReportsMissingInputs(t *testing.T) {
	ts := failIfCalled(t)

	_, stderr, err := runMini(t, ts.URL, "api", "incidents", "create-incident", "--dry-run")

	if err == nil || !strings.Contains(stderr, `"missing":["--name"]`) {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
}

func TestAPIRequiredFieldMaySetViaData(t *testing.T) {
	ts := failIfCalled(t)

	_, stderr, err := runMini(t, ts.URL, "api", "incidents", "create-incident", "--data", `{"name":"DB down"}`, "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, stderr)
	}
}

func TestAPIOnCallScheduleNeedsTimeZoneUnlessRotations(t *testing.T) {
	base := []string{"api", "signals", "create-team-on-call-schedule", "team-1", "--name", "Nights", "--dry-run"}

	t.Run("missing time zone", func(t *testing.T) {
		ts := failIfCalled(t)
		_, stderr, err := runCmdFull(t, ts.URL, base...)
		if err == nil {
			t.Fatal("expected an error")
		}
		if m := parseMissing(t, stderr); !reflect.DeepEqual(m.Missing, []string{"--time-zone"}) {
			t.Errorf("missing = %v, want [--time-zone]", m.Missing)
		}
	})
	t.Run("time zone given", func(t *testing.T) {
		ts := failIfCalled(t)
		if _, stderr, err := runCmdFull(t, ts.URL, append(base, "--time-zone", "Asia/Kolkata")...); err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, stderr)
		}
	})
	t.Run("rotations given", func(t *testing.T) {
		ts := failIfCalled(t)
		data := `{"rotations":[{"name":"r","members":[]}]}`
		if _, stderr, err := runCmdFull(t, ts.URL, append(base, "--data", data)...); err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, stderr)
		}
	})
}
