package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"testing"

	"github.com/firehydrant/fhcli/pkg/agent"
)

// withAgent makes detection return info for the rest of the test.
func withAgent(t *testing.T, info agent.Info) {
	t.Helper()
	old := detectAgent
	detectAgent = func() agent.Info { return info }
	t.Cleanup(func() { detectAgent = old })
}

// recordHeaders starts a server that answers {} and records each request's
// headers.
func recordHeaders(t *testing.T) (*httptest.Server, func() []http.Header) {
	t.Helper()
	var mu sync.Mutex
	var seen []http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "{}")
	}))
	t.Cleanup(ts.Close)
	return ts, func() []http.Header {
		mu.Lock()
		defer mu.Unlock()
		return seen
	}
}

var claudeCode = agent.Info{
	Name: "claude-code", Version: "2.1.309", Confidence: agent.High, Source: "AI_AGENT", Raw: "claude-code_2-1-309_agent",
	SessionID: "0b8f5e2a-1c3d-4e5f-8a9b-0c1d2e3f4a5b", Attended: func() *bool { b := false; return &b }(),
	Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
}

func lastHeaders(t *testing.T, seen func() []http.Header) http.Header {
	t.Helper()
	all := seen()
	if len(all) == 0 {
		t.Fatal("no request reached the server")
	}
	return all[len(all)-1]
}

func TestSDKAndRawRequestsCarryAttribution(t *testing.T) {
	withAgent(t, claudeCode)
	ts, seen := recordHeaders(t)
	platform := fmt.Sprintf("(%s; %s)", runtime.GOOS, runtime.GOARCH)

	for _, tc := range []struct {
		name string
		run  func() error
		op   string
	}{
		{"sdk", func() error { _, err := runCmd(t, ts.URL, "incidents", "get", "inc-abc"); return err }, "incidents get"},
		{"raw", func() error {
			_, _, err := runMini(t, ts.URL, "api", "incidents", "get-incident-role", "inc-1", "role-1")
			return err
		}, "api incidents get-incident-role"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			h := lastHeaders(t, seen)

			want := map[string]string{
				"User-Agent":             "fhcli/0.0.0-test " + platform + " Agent/claude-code",
				"X-Fh-Client":            "fhcli/0.0.0-test",
				"X-Fh-Operation":         tc.op,
				"X-Fh-Agent":             "name=claude-code; version=2.1.309; confidence=high; source=AI_AGENT; raw=claude-code_2-1-309_agent",
				"X-Fh-Agent-Session":     "0b8f5e2a-1c3d-4e5f-8a9b-0c1d2e3f4a5b",
				"X-Fh-Agent-Attended":    "false",
				"X-Fh-Agent-Traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			}
			for k, v := range want {
				if got := h.Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
			uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
			if id := h.Get("X-Fh-Invocation-Id"); !uuid.MatchString(id) {
				t.Errorf("X-FH-Invocation-Id = %q, want a UUID v4", id)
			}
		})
	}
}

func TestNoAgentSendsNoAgentHeaders(t *testing.T) {
	withAgent(t, agent.Info{})
	ts, seen := recordHeaders(t)

	if _, err := runCmd(t, ts.URL, "incidents", "get", "inc-abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := lastHeaders(t, seen)

	if ua := h.Get("User-Agent"); ua != fmt.Sprintf("fhcli/0.0.0-test (%s; %s)", runtime.GOOS, runtime.GOARCH) {
		t.Errorf("User-Agent = %q", ua)
	}
	for _, k := range []string{"X-Fh-Agent", "X-Fh-Agent-Session", "X-Fh-Agent-Attended", "X-Fh-Agent-Traceparent"} {
		if v := h.Get(k); v != "" {
			t.Errorf("%s = %q, want unset", k, v)
		}
	}
	if h.Get("X-Fh-Client") == "" || h.Get("X-Fh-Invocation-Id") == "" {
		t.Error("client and invocation headers are always sent")
	}
}

func TestLowConfidenceAgentHasNoUserAgentSuffix(t *testing.T) {
	withAgent(t, agent.Info{Name: "claude-code", Confidence: agent.Low, Source: "CLAUDECODE"})
	ts, seen := recordHeaders(t)

	if _, err := runCmd(t, ts.URL, "incidents", "get", "inc-abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := lastHeaders(t, seen)

	if ua := h.Get("User-Agent"); regexp.MustCompile(`Agent/`).MatchString(ua) {
		t.Errorf("User-Agent = %q, want no Agent/ suffix for a low-confidence detection", ua)
	}
	if got := h.Get("X-Fh-Agent"); got != "name=claude-code; confidence=low; source=CLAUDECODE" {
		t.Errorf("X-FH-Agent = %q", got)
	}
}

func TestDisabledDetectionIsReported(t *testing.T) {
	withAgent(t, agent.Info{Disabled: true, Source: "FHCLI_AGENT"})
	ts, seen := recordHeaders(t)

	if _, err := runCmd(t, ts.URL, "incidents", "get", "inc-abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := lastHeaders(t, seen).Get("X-Fh-Agent"); got != "detection=disabled" {
		t.Errorf("X-FH-Agent = %q, want detection=disabled", got)
	}
}

func TestOperationHeaderNeverContainsArguments(t *testing.T) {
	withAgent(t, agent.Info{})
	ts, seen := recordHeaders(t)
	path := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(path, []byte(`{"description":"secret plan"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := runMini(t, ts.URL, "api", "incidents", "create-incident",
		"--name", "DB down", "--data", "@"+path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := lastHeaders(t, seen).Get("X-Fh-Operation"); got != "api incidents create-incident" {
		t.Errorf("X-FH-Operation = %q, want the command path only", got)
	}
}

func TestCommandPathStopsAtFirstNonCommand(t *testing.T) {
	root := newRootCmd("test", "0.0.0-test", miniSpec(t), nil)

	for want, argv := range map[string][]string{
		"incidents get":                {"incidents", "get", "note"}, // "note" is an argument to get
		"incidents":                    {"incidents", "#623"},
		"api incidents":                {"api", "incidents", "--help"},
		"":                             {"not-a-command", "incidents"},
		"api incidents list-incidents": {"api", "incidents", "list-incidents", "surprise"},
	} {
		if got := commandPath(root, argv); got != want {
			t.Errorf("commandPath(%q) = %q, want %q", argv, got, want)
		}
	}
}
