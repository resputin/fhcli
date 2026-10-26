package agent

import (
	"reflect"
	"testing"
)

func detectWith(env map[string]string, files ...string) Info {
	exists := map[string]bool{}
	for _, f := range files {
		exists[f] = true
	}
	return Detect(func(k string) string { return env[k] }, func(p string) bool { return exists[p] }, true)
}

func TestDetectHarnesses(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want Info
	}{
		{"nothing", nil, Info{}},
		{
			"claude code via AI_AGENT",
			map[string]string{
				"AI_AGENT": "claude-code_2-1-309_agent", "CLAUDECODE": "1", "CLAUDE_CODE_CHILD_SESSION": "1",
				"CLAUDE_CODE_SESSION_ID": "0b8f5e2a-1c3d-4e5f-8a9b-0c1d2e3f4a5b", "CLAUDE_CODE_SESSION_ATTENDED": "0",
			},
			Info{Name: "claude-code", Version: "2.1.309", Confidence: High, Source: "AI_AGENT", Raw: "claude-code_2-1-309_agent",
				SessionID: "0b8f5e2a-1c3d-4e5f-8a9b-0c1d2e3f4a5b", Attended: boolPtr(false)},
		},
		{
			"claude code without AI_AGENT",
			map[string]string{"CLAUDECODE": "1", "CLAUDE_CODE_CHILD_SESSION": "1", "CLAUDE_CODE_SESSION_ATTENDED": "1"},
			Info{Name: "claude-code", Confidence: High, Source: "CLAUDE_CODE_CHILD_SESSION", Attended: boolPtr(true)},
		},
		{
			"github copilot cloud via AI_AGENT",
			map[string]string{"AI_AGENT": "github_copilot_cloud_agent", "COPILOT_AGENT": "1", "COPILOT_AGENT_JOB_ID": "job-42"},
			Info{Name: "github-copilot", Surface: "cloud", Confidence: High, Source: "AI_AGENT", Raw: "github_copilot_cloud_agent", SessionID: "job-42"},
		},
		{
			"unknown AI_AGENT becomes its slug",
			map[string]string{"AI_AGENT": "My_Tool"},
			Info{Name: "my-tool", Confidence: High, Source: "AI_AGENT", Raw: "My_Tool"},
		},
		{
			"amp before claude code (amp sets claude markers too)",
			map[string]string{"AGENT": "amp", "CLAUDECODE": "1", "CLAUDE_CODE_CHILD_SESSION": "1", "AMP_CURRENT_THREAD_ID": "T-abc123"},
			Info{Name: "amp", Confidence: High, Source: "AGENT", SessionID: "T-abc123"},
		},
		{
			"codex",
			map[string]string{"CODEX_THREAD_ID": "thread-1", "CODEX_SESSION_ID": "sess-1", "CODEX_VERSION": "0.46.0"},
			Info{Name: "codex", Version: "0.46.0", Confidence: High, Source: "CODEX_THREAD_ID", SessionID: "sess-1"},
		},
		{"kilo", map[string]string{"KILO": "1"}, Info{Name: "kilo", Confidence: High, Source: "KILO"}},
		{
			"opencode",
			map[string]string{"OPENCODE": "1", "OPENCODE_SESSION_ID": "ses_123"},
			Info{Name: "opencode", Confidence: High, Source: "OPENCODE", SessionID: "ses_123"},
		},
		{"crush", map[string]string{"CRUSH": "1"}, Info{Name: "crush", Confidence: High, Source: "CRUSH"}},
		{
			"qwen code before gemini (qwen is a gemini fork)",
			map[string]string{"QWEN_CODE": "1", "GEMINI_CLI": "1", "QWEN_CODE_SESSION_ID": "q-1", "QWEN_CODE_STARTUP_VERSION": "0.0.14"},
			Info{Name: "qwen-code", Version: "0.0.14", Confidence: High, Source: "QWEN_CODE", SessionID: "q-1"},
		},
		{"gemini cli", map[string]string{"GEMINI_CLI": "1"}, Info{Name: "gemini-cli", Confidence: High, Source: "GEMINI_CLI"}},
		{
			"copilot cli",
			map[string]string{"COPILOT_CLI": "1", "COPILOT_CLI_BINARY_VERSION": "0.0.339"},
			Info{Name: "github-copilot", Version: "0.0.339", Confidence: High, Source: "COPILOT_CLI"},
		},
		{"cursor agent", map[string]string{"CURSOR_AGENT": "1", "CURSOR_TRACE_ID": "x"}, Info{Name: "cursor", Confidence: High, Source: "CURSOR_AGENT"}},
		{
			"cursor agent exec host",
			map[string]string{"CURSOR_EXTENSION_HOST_ROLE": "agent-exec"},
			Info{Name: "cursor", Confidence: High, Source: "CURSOR_EXTENSION_HOST_ROLE"},
		},
		{"auggie", map[string]string{"AUGMENT_AGENT": "1"}, Info{Name: "auggie", Confidence: High, Source: "AUGMENT_AGENT"}},
		{"cline", map[string]string{"CLINE_ACTIVE": "true"}, Info{Name: "cline", Confidence: High, Source: "CLINE_ACTIVE"}},
		{"roo", map[string]string{"ROO_ACTIVE": "true"}, Info{Name: "roo", Confidence: High, Source: "ROO_ACTIVE"}},
		{
			"goose",
			map[string]string{"GOOSE_TERMINAL": "1", "AGENT_SESSION_ID": "20251026_1"},
			Info{Name: "goose", Confidence: High, Source: "GOOSE_TERMINAL", SessionID: "20251026_1"},
		},
		{
			"current goose builds set only AGENT_SESSION_ID",
			map[string]string{"AGENT_SESSION_ID": "20251026_1"},
			Info{Name: "goose", Confidence: High, Source: "AGENT_SESSION_ID", SessionID: "20251026_1"},
		},
		{"antigravity", map[string]string{"ANTIGRAVITY_AGENT": "1"}, Info{Name: "antigravity", Confidence: High, Source: "ANTIGRAVITY_AGENT"}},
		{
			"kiro",
			map[string]string{"AGENT_CONTEXT_OUT": "/tmp/ctx", "AGENT_DISPLAY_OUT": "/tmp/display"},
			Info{Name: "kiro", Confidence: High, Source: "AGENT_CONTEXT_OUT"},
		},
		{
			"traceparent is passed along",
			map[string]string{"CRUSH": "1", "TRACEPARENT": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
			Info{Name: "crush", Confidence: High, Source: "CRUSH", Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		},
		{
			"malformed metadata is dropped",
			map[string]string{"OPENCODE": "1", "OPENCODE_SESSION_ID": "has spaces", "TRACEPARENT": "garbage"},
			Info{Name: "opencode", Confidence: High, Source: "OPENCODE"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectWith(tc.env); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestDetectPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want Info
	}{
		{
			"FHCLI_AGENT names a tool and wins over everything",
			map[string]string{"FHCLI_AGENT": "deploy-bot", "AI_AGENT": "claude-code_2-1-309_agent", "CRUSH": "1"},
			Info{Name: "deploy-bot", Confidence: High, Source: "FHCLI_AGENT", Raw: "deploy-bot"},
		},
		{
			"falsy FHCLI_AGENT disables detection, reported",
			map[string]string{"FHCLI_AGENT": "off", "CRUSH": "1"},
			Info{Disabled: true, Source: "FHCLI_AGENT"},
		},
		{
			"AI_AGENT wins over markers",
			map[string]string{"AI_AGENT": "gemini-cli", "CRUSH": "1"},
			Info{Name: "gemini-cli", Confidence: High, Source: "AI_AGENT", Raw: "gemini-cli"},
		},
		{
			"falsy AI_AGENT means no agent",
			map[string]string{"AI_AGENT": " false ", "CRUSH": "1"},
			Info{},
		},
		{
			"invalid AI_AGENT is ignored, markers still apply",
			map[string]string{"AI_AGENT": "claude code!", "CRUSH": "1"},
			Info{Name: "crush", Confidence: High, Source: "CRUSH"},
		},
		{
			"invalid FHCLI_AGENT is ignored",
			map[string]string{"FHCLI_AGENT": "has space", "KILO": "1"},
			Info{Name: "kilo", Confidence: High, Source: "KILO"},
		},
		{
			"falsy marker values don't count",
			map[string]string{"KILO": "0", "GEMINI_CLI": "false"},
			Info{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectWith(tc.env); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestDetectLowConfidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   map[string]string
		files []string
		tty   bool
		want  Info
	}{
		{"IDE terminal sets CLAUDECODE alone", map[string]string{"CLAUDECODE": "1"}, nil, true,
			Info{Name: "claude-code", Confidence: Low, Source: "CLAUDECODE"}},
		{"cursor IDE trace id", map[string]string{"CURSOR_TRACE_ID": "abc"}, nil, true,
			Info{Name: "cursor", Confidence: Low, Source: "CURSOR_TRACE_ID"}},
		{"kiro terminal without a TTY", map[string]string{"TERM_PROGRAM": "kiro"}, nil, false,
			Info{Name: "kiro", Confidence: Low, Source: "TERM_PROGRAM"}},
		{"kiro terminal with a TTY is a person", map[string]string{"TERM_PROGRAM": "kiro"}, nil, true, Info{}},
		{"devin machine", nil, []string{"/opt/.devin"}, true, Info{Name: "devin", Confidence: Low, Source: "/opt/.devin"}},
		{"replit", map[string]string{"REPL_ID": "x"}, nil, true, Info{Name: "replit", Confidence: Low, Source: "REPL_ID"}},
		{"goose provider config", map[string]string{"GOOSE_PROVIDER": "anthropic"}, nil, true,
			Info{Name: "goose", Confidence: Low, Source: "GOOSE_PROVIDER"}},
		{"bare AGENT=1", map[string]string{"AGENT": "1"}, nil, true, Info{Name: "unknown", Confidence: Low, Source: "AGENT"}},
		{"vscode terminal is not an agent", map[string]string{"TERM_PROGRAM": "vscode"}, nil, true, Info{}},
		{"zed terminal is not an agent", map[string]string{"TERM_PROGRAM": "zed"}, nil, true, Info{}},
		{"copilot config vars are not markers", map[string]string{"COPILOT_MODEL": "x", "COPILOT_ALLOW_ALL": "1", "COPILOT_GITHUB_TOKEN": "t"}, nil, true, Info{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exists := map[string]bool{}
			for _, f := range tc.files {
				exists[f] = true
			}
			got := Detect(func(k string) string { return tc.env[k] }, func(p string) bool { return exists[p] }, tc.tty)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestDetectedNamesAreKnown(t *testing.T) {
	known := map[string]bool{}
	for _, n := range KnownAgents {
		known[n] = true
	}
	for _, m := range markers {
		if !known[m.name] {
			t.Errorf("marker agent %q is missing from KnownAgents", m.name)
		}
	}
	for _, m := range lowMarkers {
		if m.name != "unknown" && !known[m.name] {
			t.Errorf("low-confidence agent %q is missing from KnownAgents", m.name)
		}
	}
}
