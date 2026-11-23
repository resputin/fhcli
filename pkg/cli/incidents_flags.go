package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/firehydrant/firehydrant-go-sdk/models/components"
	"github.com/urfave/cli/v3"
)

// textFlagUsage is appended to curated text flags that read @file and @-,
// matching what `fhcli api` string body flags advertise.
const textFlagUsage = " (accepts @file, or @- for stdin)"

// textFlag is a curated text flag's value, reading @path or @- (stdin) the
// way `fhcli api` string body flags do. Without this, "--body @note.md"
// posted the literal path. stdinUsed is shared across a command's flags so
// only one of them can read stdin.
func textFlag(cmd *cli.Command, name string, stdinUsed *bool) (string, error) {
	v := cmd.String(name)
	if !strings.HasPrefix(v, "@") {
		return v, nil
	}
	return readFlagFile(name, v, stdinUsed)
}

// noteVisibilities are the CreateIncidentNote visibility values.
var noteVisibilities = []string{
	string(components.CreateIncidentNoteVisibilityPrivateToOrg),
	string(components.CreateIncidentNoteVisibilityOpenToPublic),
	string(components.CreateIncidentNoteVisibilityInternalStatusPage),
}

func noteVisibility(v string) (components.CreateIncidentNoteVisibility, error) {
	if !slices.Contains(noteVisibilities, v) {
		return "", fmt.Errorf("--visibility %q: must be one of %s", v, strings.Join(noteVisibilities, ", "))
	}
	return components.CreateIncidentNoteVisibility(v), nil
}

// incidentEventTypes are the timeline event types: the ones GET
// /v1/incidents/{id}/events documents for its types filter
// (laddertruck app/api/public_api/v1/incident_events.rb) plus the other
// Event:: classes in laddertruck app/models/event/.
var incidentEventTypes = []string{
	"add_task_list", "alert_event", "alert_linked", "bulk_milestone_update",
	"bulk_update", "change_type", "changelog_entry", "chat_message",
	"chat_message_summary", "child_changed", "children_changed",
	"conference_bridge_participant", "conference_bridge_summary",
	"external_link", "general_update", "generic_chat_message",
	"generic_resource_change", "handoff", "impact_update",
	"incident_attachment", "incident_restriction", "incident_status",
	"new_related_change_event", "note", "parent_changed", "role_update",
	"root_cause", "runbook_attachment", "runbook_step_execution_update",
	"task_update", "team_assignment", "ticket_update",
}

// normalizeEventTypes turns a --types list into the form the API matches.
// The API documents short names (chat_message), but it classifies each one
// and compares it with the stored event_type (Event::ChatMessage), so only
// "event/chat_message" matches: the short form silently returns no events.
// Short and "event/" names are both accepted; anything else is an error
// rather than an empty result.
func normalizeEventTypes(list string) (string, error) {
	var out []string
	for _, t := range strings.Split(list, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		short := strings.TrimPrefix(t, "event/")
		if !slices.Contains(incidentEventTypes, short) {
			return "", fmt.Errorf("--types: unknown event type %q (one of: %s)", t, strings.Join(incidentEventTypes, ", "))
		}
		out = append(out, "event/"+short)
	}
	return strings.Join(out, ","), nil
}
