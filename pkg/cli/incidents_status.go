package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

// incidentStatusPage is an entry of GET /v1/incidents/{id}/status_pages.
// Its own id is the incident's link to the page; the impact endpoint's
// status_pages targets the page's connection instead, which is
// integration.id (laddertruck Incidents::StatusPage belongs_to :integration,
// polymorphic, and StatusPageReader#filter_connections looks it up).
type incidentStatusPage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	URL         string `json:"url"`
	Integration struct {
		ID              string `json:"id"`
		IntegrationSlug string `json:"integration_slug"`
	} `json:"integration"`
}

// label is how a page is named in output and errors.
func (p incidentStatusPage) label() string {
	name := p.DisplayName
	if name == "" {
		name = p.Name
	}
	if p.URL != "" {
		return fmt.Sprintf("%s (%s)", name, p.URL)
	}
	return name
}

// matches reports whether ref (from --page) names this page: its name,
// display name, URL (with or without scheme), its id or its connection id.
func (p incidentStatusPage) matches(ref string) bool {
	ref = strings.TrimSpace(ref)
	for _, v := range []string{p.ID, p.Integration.ID, p.Name, p.DisplayName, p.URL, stripScheme(p.URL)} {
		if v != "" && strings.EqualFold(v, ref) {
			return true
		}
	}
	return p.URL != "" && strings.EqualFold(stripScheme(p.URL), stripScheme(ref))
}

func stripScheme(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	return strings.TrimRight(u, "/")
}

// statusPageTarget is one entry of the impact endpoint's status_pages.
type statusPageTarget struct {
	ID              string `json:"id"`
	IntegrationSlug string `json:"integration_slug"`
}

func listIncidentStatusPages(ctx context.Context, client *rawClient, incidentID string) ([]incidentStatusPage, error) {
	query := url.Values{"per_page": {"100"}}
	body, err := client.get(ctx, "/v1/incidents/"+url.PathEscape(incidentID)+"/status_pages", query)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []incidentStatusPage `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("list status pages: %w", err)
	}
	return resp.Data, nil
}

func incidentsStatusPagesCmd(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	client, err := getRawClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}
	body, err := client.get(ctx, "/v1/incidents/"+url.PathEscape(incidentID)+"/status_pages", url.Values{"per_page": {"100"}})
	if err != nil {
		return failCmd(cmd, err)
	}
	return outputResult(cmd, body)
}

// selectStatusPages picks the pages an update goes to. Exactly one of
// refs, all and internalOnly must be given, because the API's default (no
// status_pages) silently skips every public page: in incident #671 an
// update meant for the public page reached only the internal one.
func selectStatusPages(attached []incidentStatusPage, refs []string, all, internalOnly bool) ([]incidentStatusPage, error) {
	chosen := 0
	for _, set := range []bool{len(refs) > 0, all, internalOnly} {
		if set {
			chosen++
		}
	}
	if chosen != 1 {
		return nil, fmt.Errorf("choose where the update goes: --page <name|url|id> (repeatable), --all-pages, or --internal-only%s", attachedHint(attached))
	}
	switch {
	case internalOnly:
		return nil, nil
	case all:
		if len(attached) == 0 {
			return nil, errors.New("no status pages are attached to this incident; attach one with `fhcli api incidents create-incident-status-page`, or use --internal-only")
		}
		return attached, nil
	}

	var out []incidentStatusPage
	for _, ref := range refs {
		var found *incidentStatusPage
		for i := range attached {
			if attached[i].matches(ref) {
				found = &attached[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("--page %q is not attached to this incident%s", ref, attachedHint(attached))
		}
		out = append(out, *found)
	}
	return out, nil
}

func attachedHint(attached []incidentStatusPage) string {
	if len(attached) == 0 {
		return " (no status pages are attached to this incident)"
	}
	labels := make([]string, len(attached))
	for i, p := range attached {
		labels[i] = p.label()
	}
	return "; attached pages: " + strings.Join(labels, ", ")
}

// statusUpdateOutput wraps the API response with where the update went,
// which the response itself doesn't say.
type statusUpdateOutput struct {
	PublishedTo []string        `json:"published_to"`
	Incident    json.RawMessage `json:"incident"`
}

func incidentsStatusUpdateCmd(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	note, err := textFlag(cmd, "note", new(bool))
	if err != nil {
		return failCmd(cmd, err)
	}
	if strings.TrimSpace(note) == "" {
		return failCmd(cmd, errors.New("--note is empty"))
	}
	client, err := getRawClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}

	attached, err := listIncidentStatusPages(ctx, client, incidentID)
	if err != nil {
		return failCmd(cmd, err)
	}
	pages, err := selectStatusPages(attached, cmd.StringSlice("page"), cmd.Bool("all-pages"), cmd.Bool("internal-only"))
	if err != nil {
		return failCmd(cmd, err)
	}

	body := map[string]any{"note": note}
	if m := cmd.String("milestone"); m != "" {
		body["milestone"] = m
	}
	publishedTo := []string{"internal status page"}
	if len(pages) > 0 {
		targets := make([]statusPageTarget, len(pages))
		publishedTo = make([]string, len(pages))
		for i, p := range pages {
			if p.Integration.ID == "" {
				return failCmd(cmd, fmt.Errorf("status page %s has no connection id in the API response", p.label()))
			}
			slug := p.Integration.IntegrationSlug
			if slug == "" {
				slug = "nunc"
			}
			targets[i] = statusPageTarget{ID: p.Integration.ID, IntegrationSlug: slug}
			publishedTo[i] = p.label()
		}
		body["status_pages"] = targets
		if cmd.Bool("no-email") {
			body["notify_email_subscribers"] = false
		}
	}

	path := "/v1/incidents/" + url.PathEscape(incidentID) + "/impact"
	if cmd.Bool("dry-run") {
		var m map[string]any
		raw, _ := json.Marshal(body)
		_ = json.Unmarshal(raw, &m)
		return outputJSON(os.Stdout, dryRunOutput{
			Method: http.MethodPatch,
			URL:    requestHost(ctx, cmd) + path,
			Body:   m,
			Note:   "publishes to: " + strings.Join(publishedTo, ", "),
		})
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return failCmd(cmd, err)
	}
	resp, err := client.do(ctx, http.MethodPatch, path, nil, raw)
	if err != nil {
		return failCmd(cmd, err)
	}
	return outputResult(cmd, statusUpdateOutput{PublishedTo: publishedTo, Incident: resp})
}
