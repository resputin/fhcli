// Package agent detects whether an AI coding agent is running fhcli, so
// requests can say which agent acted. Detection is a client-reported hint
// for audit and observability, never for authorization: any process can set
// these variables.
//
// The order follows the AI_AGENT proposal (agents.md#136) and established
// detectors (gh's internal/agents, Vercel detect-agent, unjs std-env).
package agent

import (
	"os"
	"regexp"
	"strings"
)

type Confidence string

const (
	High Confidence = "high"
	Low  Confidence = "low"
)

// Info is what was detected. The zero value means no agent.
type Info struct {
	Name       string
	Version    string
	Surface    string // e.g. "cloud" or "vscode" for GitHub Copilot
	Confidence Confidence
	Source     string // the variable (or file) that identified the agent
	Raw        string // the FHCLI_AGENT or AI_AGENT value as given
	SessionID  string
	Attended   *bool // nil when the harness doesn't say
	// Traceparent is the agent's W3C trace context, passed along as data.
	Traceparent string
	// Disabled is set when FHCLI_AGENT is falsy. It is reported rather than
	// silently dropped, because attribution is for audit.
	Disabled bool
}

// KnownAgents are the names detection can produce. Laddertruck mirrors this
// list (RequestAttribution::KNOWN_AGENTS) to bound its fh.agent log facet;
// keep the two in sync.
var KnownAgents = []string{
	"amp", "claude-code", "codex", "kilo", "opencode", "crush", "qwen-code", "gemini-cli",
	"github-copilot", "cursor", "auggie", "cline", "roo", "goose", "antigravity", "kiro",
	"devin", "replit",
}

var (
	validValue  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	validEnv    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	validID     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	traceparent = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)
	// claude-code_2-1-309_agent → claude-code, 2-1-309
	versioned = regexp.MustCompile(`^(.+?)_(\d+(?:-\d+)*)_agent$`)
	copilot   = regexp.MustCompile(`^github_copilot_([a-z0-9]+)_agent$`)
)

// probe is what detection may look at.
type probe struct {
	getenv func(string) string
	exists func(string) bool
	isTTY  bool
}

// marker identifies an agent from what it sets in processes it spawns.
// match returns the variable (or file) that matched, or "".
type marker struct {
	name  string
	match func(probe) string
}

func set(keys ...string) func(probe) string {
	return func(p probe) string {
		for _, k := range keys {
			if truthy(p.getenv(k)) {
				return k
			}
		}
		return ""
	}
}

func equals(key, value string) func(probe) string {
	return func(p probe) string {
		if p.getenv(key) == value {
			return key
		}
		return ""
	}
}

func either(fs ...func(probe) string) func(probe) string {
	return func(p probe) string {
		for _, f := range fs {
			if k := f(p); k != "" {
				return k
			}
		}
		return ""
	}
}

// markers are checked in order, first match wins. Forks and shims come
// before their parents, since they also set the parent's markers (Amp sets
// Claude Code's; Qwen Code sets Gemini CLI's).
var markers = []marker{
	{"amp", equals("AGENT", "amp")},
	{"claude-code", set("CLAUDE_CODE_CHILD_SESSION")},
	{"codex", set("CODEX_THREAD_ID", "CODEX_SESSION_ID", "CODEX_CI")},
	{"kilo", set("KILO")},
	{"opencode", set("OPENCODE")},
	{"crush", set("CRUSH")},
	{"qwen-code", set("QWEN_CODE")},
	{"gemini-cli", set("GEMINI_CLI")},
	{"github-copilot", set("COPILOT_CLI", "COPILOT_AGENT")},
	{"cursor", either(set("CURSOR_AGENT"), equals("CURSOR_EXTENSION_HOST_ROLE", "agent-exec"))},
	{"auggie", set("AUGMENT_AGENT")},
	{"cline", equals("CLINE_ACTIVE", "true")},
	{"roo", equals("ROO_ACTIVE", "true")},
	{"goose", either(set("GOOSE_TERMINAL"), equals("AGENT", "goose"), set("AGENT_SESSION_ID"))},
	{"antigravity", set("ANTIGRAVITY_AGENT")},
	{"kiro", func(p probe) string {
		if p.getenv("AGENT_CONTEXT_OUT") != "" && p.getenv("AGENT_DISPLAY_OUT") != "" {
			return "AGENT_CONTEXT_OUT"
		}
		return ""
	}},
}

// lowMarkers are reported with Low confidence: they are also set where a
// person is typing (IDE terminals) or describe configuration, not a spawn.
var lowMarkers = []marker{
	{"claude-code", set("CLAUDECODE")},
	{"cursor", set("CURSOR_TRACE_ID")},
	{"kiro", func(p probe) string {
		if p.getenv("TERM_PROGRAM") == "kiro" && !p.isTTY {
			return "TERM_PROGRAM"
		}
		return ""
	}},
	{"devin", func(p probe) string {
		if p.exists("/opt/.devin") {
			return "/opt/.devin"
		}
		return ""
	}},
	{"replit", set("REPL_ID")},
	{"goose", set("GOOSE_PROVIDER")},
	{"unknown", equals("AGENT", "1")},
}

// FromEnvironment detects the agent running this process.
func FromEnvironment() Info {
	tty := false
	if fi, err := os.Stdin.Stat(); err == nil {
		tty = fi.Mode()&os.ModeCharDevice != 0
	}
	return Detect(os.Getenv, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}, tty)
}

// Detect identifies the agent from getenv, exists (a file check) and whether
// stdin is a terminal. It does no I/O of its own.
func Detect(getenv func(string) string, exists func(string) bool, isTTY bool) Info {
	if raw := strings.TrimSpace(getenv("FHCLI_AGENT")); raw != "" {
		if falsy(raw) {
			return Info{Disabled: true, Source: "FHCLI_AGENT"}
		}
		if validEnv.MatchString(raw) {
			return withMetadata(getenv, Info{Name: slug(raw), Confidence: High, Source: "FHCLI_AGENT", Raw: raw})
		}
	}

	if raw := strings.TrimSpace(getenv("AI_AGENT")); raw != "" {
		if falsy(raw) {
			return Info{}
		}
		if validEnv.MatchString(raw) {
			return withMetadata(getenv, fromAIAgent(raw))
		}
	}

	p := probe{getenv, exists, isTTY}
	for _, m := range markers {
		if source := m.match(p); source != "" {
			return withMetadata(getenv, Info{Name: m.name, Confidence: High, Source: source})
		}
	}
	for _, m := range lowMarkers {
		if source := m.match(p); source != "" {
			return withMetadata(getenv, Info{Name: m.name, Confidence: Low, Source: source})
		}
	}
	return Info{}
}

// fromAIAgent normalizes an AI_AGENT value: claude-code_2-1-309_agent is
// claude-code 2.1.309, github_copilot_cloud_agent is github-copilot on the
// cloud surface, and anything else is its slug.
func fromAIAgent(raw string) Info {
	info := Info{Confidence: High, Source: "AI_AGENT", Raw: raw}
	lower := strings.ToLower(raw)
	if m := copilot.FindStringSubmatch(lower); m != nil {
		info.Name, info.Surface = "github-copilot", m[1]
	} else if m := versioned.FindStringSubmatch(lower); m != nil {
		info.Name, info.Version = slug(m[1]), strings.ReplaceAll(m[2], "-", ".")
	} else {
		info.Name = slug(strings.TrimSuffix(lower, "_agent"))
	}
	return info
}

// withMetadata fills the session, version, attended state and trace context
// the identified harness exposes. Values that don't look like IDs are
// dropped.
func withMetadata(getenv func(string) string, info Info) Info {
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := getenv(k); v != "" {
				return v
			}
		}
		return ""
	}

	var session, version string
	switch info.Name {
	case "claude-code":
		session = getenv("CLAUDE_CODE_SESSION_ID")
		switch getenv("CLAUDE_CODE_SESSION_ATTENDED") {
		case "1":
			info.Attended = boolPtr(true)
		case "0":
			info.Attended = boolPtr(false)
		}
	case "codex":
		// CODEX_SESSION_ID is the root session; threads belong to it.
		session = first("CODEX_SESSION_ID", "CODEX_THREAD_ID")
		version = getenv("CODEX_VERSION")
	case "github-copilot":
		session = first("COPILOT_AGENT_SESSION_ID", "COPILOT_AGENT_JOB_ID")
		version = getenv("COPILOT_CLI_BINARY_VERSION")
	case "opencode":
		session = getenv("OPENCODE_SESSION_ID")
	case "qwen-code":
		session = getenv("QWEN_CODE_SESSION_ID")
		version = getenv("QWEN_CODE_STARTUP_VERSION")
	case "goose":
		session = getenv("AGENT_SESSION_ID")
	case "amp":
		session = getenv("AMP_CURRENT_THREAD_ID")
	}
	if validID.MatchString(session) {
		info.SessionID = session
	}
	if info.Version == "" && validValue.MatchString(version) {
		info.Version = version
	}
	if tp := getenv("TRACEPARENT"); traceparent.MatchString(tp) {
		info.Traceparent = tp
	}
	return info
}

func falsy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}

func truthy(v string) bool {
	return strings.TrimSpace(v) != "" && !falsy(v)
}

func slug(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "_", "-")
}

func boolPtr(b bool) *bool { return &b }
