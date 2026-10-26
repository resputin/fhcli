package cli

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/firehydrant/fhcli/pkg/agent"
	"github.com/urfave/cli/v3"
)

// Every request says which client sent it, which command ran, and which AI
// agent (if any) ran the command, so FireHydrant's audit log and telemetry
// can attribute it. Agent values are client-reported: the server labels
// them as such and never authorizes on them.

// detectAgent is replaced in tests.
var detectAgent = agent.FromEnvironment

// cliVersion is the version reported in User-Agent and X-FH-Client.
var cliVersion = "dev"

// invocationID identifies this process's requests, so the server can group
// the calls one command made.
var invocationID = newInvocationID()

var (
	validHeaderValue = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	validOperation   = regexp.MustCompile(`^[a-z0-9 _.-]{1,128}$`)
)

type attribution struct {
	operation string
	agent     agent.Info
}

// currentAttribution is set by rootBefore for the running command.
var currentAttribution atomic.Pointer[attribution]

func (a *attribution) headers() http.Header {
	h := http.Header{}
	ua := fmt.Sprintf("fhcli/%s (%s; %s)", cliVersion, runtime.GOOS, runtime.GOARCH)
	if a.agent.Confidence == agent.High && validHeaderValue.MatchString(a.agent.Name) {
		ua += " Agent/" + a.agent.Name
	}
	h.Set("User-Agent", ua)
	h.Set("X-FH-Client", "fhcli/"+cliVersion)
	h.Set("X-FH-Invocation-Id", invocationID)
	if validOperation.MatchString(a.operation) {
		h.Set("X-FH-Operation", a.operation)
	}
	if v := agentHeader(a.agent); v != "" {
		h.Set("X-FH-Agent", v)
	}
	if a.agent.SessionID != "" {
		h.Set("X-FH-Agent-Session", a.agent.SessionID)
	}
	if a.agent.Attended != nil {
		h.Set("X-FH-Agent-Attended", strconv.FormatBool(*a.agent.Attended))
	}
	if a.agent.Traceparent != "" {
		h.Set("X-FH-Agent-Traceparent", a.agent.Traceparent)
	}
	return h
}

// report is the attribution as whoami shows it.
func (a *attribution) report() (client map[string]any, agentInfo map[string]any) {
	client = map[string]any{"name": "fhcli", "version": cliVersion, "invocation_id": invocationID}
	info := a.agent
	if info.Disabled {
		return client, map[string]any{"detection": "disabled"}
	}
	if info.Name == "" {
		return client, nil
	}
	agentInfo = map[string]any{}
	for k, v := range map[string]string{
		"name": info.Name, "version": info.Version, "surface": info.Surface, "confidence": string(info.Confidence),
		"source": info.Source, "raw": info.Raw, "session_id": info.SessionID, "traceparent": info.Traceparent,
	} {
		if v != "" {
			agentInfo[k] = v
		}
	}
	if info.Attended != nil {
		agentInfo["attended"] = *info.Attended
	}
	return client, agentInfo
}

// agentHeader formats X-FH-Agent as "key=value; …", omitting empty values
// and any the server would reject.
func agentHeader(info agent.Info) string {
	if info.Disabled {
		return "detection=disabled"
	}
	var pairs []string
	for _, kv := range [][2]string{
		{"name", info.Name},
		{"version", info.Version},
		{"confidence", string(info.Confidence)},
		{"source", info.Source},
		{"raw", info.Raw},
		{"surface", info.Surface},
	} {
		if validHeaderValue.MatchString(kv[1]) {
			pairs = append(pairs, kv[0]+"="+kv[1])
		}
	}
	return strings.Join(pairs, "; ")
}

// attributionTransport adds the attribution headers to every API request,
// overriding the SDK's own User-Agent.
type attributionTransport struct {
	next http.RoundTripper
}

func (t attributionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if a := currentAttribution.Load(); a != nil {
		r = r.Clone(r.Context())
		for k, v := range a.headers() {
			r.Header[k] = v
		}
	}
	return t.next.RoundTrip(r)
}

// commandPath is the names of the subcommands args selects, e.g.
// "api incidents create-incident-note". It stops at the first token that
// isn't a subcommand, so arguments, flags and their values never appear.
func commandPath(root *cli.Command, args []string) string {
	var names []string
	cur := root
	for _, arg := range args {
		sub := cur.Command(arg)
		if sub == nil {
			break
		}
		names = append(names, sub.Name)
		cur = sub
	}
	return strings.Join(names, " ")
}

func newInvocationID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
