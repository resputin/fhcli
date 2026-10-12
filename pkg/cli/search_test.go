package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type searchOutput struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Summary string   `json:"summary"`
	Score   float64  `json:"score"`
}

func runSearch(t *testing.T, args ...string) ([]searchOutput, string, error) {
	t.Helper()
	stdout, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), append([]string{"fhcli", "search"}, args...)...)
	var out []searchOutput
	if err == nil {
		if jerr := json.Unmarshal([]byte(stdout), &out); jerr != nil {
			t.Fatalf("stdout is not a JSON array: %v\n%s", jerr, stdout)
		}
	}
	return out, stderr, err
}

func TestSearchCmdReturnsRunnableCommands(t *testing.T) {
	out, stderr, err := runSearch(t, "severity", "matrix")

	if err != nil {
		t.Fatalf("err = %v, stderr = %s", err, stderr)
	}
	if len(out) == 0 {
		t.Fatal("no results")
	}
	first := out[0]
	if first.Command != "fhcli api incident-settings get-severity-matrix" && first.Command != "fhcli api incident-settings update-severity-matrix" {
		t.Errorf("first command = %q", first.Command)
	}
	if first.Path != "/v1/severity_matrix" || first.Summary == "" || first.Method == "" || first.Score <= 0 {
		t.Errorf("first result = %+v", first)
	}
	if first.Args == nil {
		t.Error("args should be [] for an operation with no path params, not null")
	}
}

func TestSearchCmdListsPathParamsAsArgs(t *testing.T) {
	out, _, err := runSearch(t, "add", "note", "to", "incident")

	if err != nil || len(out) == 0 {
		t.Fatalf("err = %v, out = %v", err, out)
	}
	if out[0].Command != "fhcli api incidents create-incident-note" || strings.Join(out[0].Args, ",") != "incident_id" {
		t.Errorf("first = %+v, want create-incident-note with args [incident_id]", out[0])
	}
}

func TestSearchCmdLimit(t *testing.T) {
	out, _, err := runSearch(t, "--limit", "3", "incident")

	if err != nil || len(out) != 3 {
		t.Errorf("len = %d, err = %v; want 3", len(out), err)
	}
}

func TestSearchCmdNoMatchesPrintsEmptyArray(t *testing.T) {
	stdout, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "search", "kubernetesxyz")

	if err != nil || strings.TrimSpace(stdout) != "[]" {
		t.Errorf("stdout = %q, err = %v; want []", stdout, err)
	}
}

func TestSearchCmdRequiresAQuery(t *testing.T) {
	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "search")

	if err == nil || !strings.Contains(stderr, "fhcli search severity matrix") {
		t.Errorf("err = %v, stderr = %s; want a usage error with an example", err, stderr)
	}
}

func TestSearchCmdNeedsNoCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, stderr, err := runSearch(t, "incident")

	if err != nil || strings.Contains(stderr, "API token") {
		t.Errorf("err = %v, stderr = %s; search must work without a token or login", err, stderr)
	}
}

func TestSearchCmdInteractiveTable(t *testing.T) {
	stdout, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "-i", "search", "severity", "matrix")

	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout), "[") || !strings.Contains(stdout, "fhcli api incident-settings get-severity-matrix") || !strings.Contains(stdout, "Get severity matrix") {
		t.Errorf("stdout = %q, want a command/summary table", stdout)
	}
}

func TestSearchCmdReportsSpecLoadError(t *testing.T) {
	root := newRootCmd("test", "0.0.0-test", nil, errors.New("override spec is invalid; run `fhcli spec update` or delete the file"))

	_, stderr, err := runRoot(t, root, "fhcli", "search", "incident")

	if err == nil || !strings.Contains(stderr, "fhcli spec update") {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
}
