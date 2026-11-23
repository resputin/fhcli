package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	firehydrantgosdk "github.com/firehydrant/firehydrant-go-sdk"
	"github.com/firehydrant/firehydrant-go-sdk/models/components"
	"github.com/firehydrant/firehydrant-go-sdk/models/operations"
	"github.com/urfave/cli/v3"
)

// triageResult combines the key data a responder needs when first paged.
type triageResult struct {
	Incident  any `json:"incident"`
	Alerts    any `json:"alerts"`
	Summaries any `json:"summaries"`
}

// contextResult enriches an incident with full service and team details.
type contextResult struct {
	Incident any               `json:"incident"`
	Services []enrichedService `json:"services"`
	Teams    []enrichedTeam    `json:"teams"`
}

type enrichedService struct {
	Service        any           `json:"service"`
	DashboardLinks []serviceLink `json:"dashboard_links"`
}

type serviceLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type enrichedTeam struct {
	Team            any `json:"team"`
	OnCallSchedules any `json:"on_call_schedules"`
}

func newIncidentsCmd() *cli.Command {
	return &cli.Command{
		Name:  "incidents",
		Usage: "manage FireHydrant incidents",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list incidents",
				Action: incidentsListCmd,
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "page",
						Usage: "page number",
						Value: 1,
					},
					&cli.IntFlag{
						Name:  "per-page",
						Usage: "results per page",
						Value: 20,
					},
					&cli.StringFlag{
						Name:    "query",
						Aliases: []string{"q"},
						Usage:   "search query",
					},
					&cli.StringFlag{
						Name:  "status",
						Usage: "filter by status",
					},
					&cli.StringFlag{
						Name:  "severity",
						Usage: "filter by severity",
					},
				},
			},
			{
				Name:   "get",
				Usage:  "get incident details",
				Action: incidentsGetCmd,
			},
			{
				Name:   "note",
				Usage:  "post a note on an incident's timeline (to update status pages, use status-update)",
				Action: incidentsNoteCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "body",
						Aliases:  []string{"b"},
						Usage:    "note body text" + textFlagUsage,
						Required: true,
					},
					&cli.StringFlag{
						Name:  "visibility",
						Usage: "who can see the note: " + strings.Join(noteVisibilities, ", ") + "; open_to_public is visible outside your organization",
						Value: "private_to_org",
					},
				},
			},
			{
				Name:   "chat",
				Usage:  "post a chat message on an incident",
				Action: incidentsChatCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "body",
						Aliases:  []string{"b"},
						Usage:    "message body text" + textFlagUsage,
						Required: true,
					},
				},
			},
			{
				Name:   "alerts",
				Usage:  "list alerts attached to an incident",
				Action: incidentsAlertsCmd,
			},
			{
				Name:   "summary",
				Usage:  "get AI-generated audience summaries for an incident",
				Action: incidentsSummaryCmd,
			},
			{
				Name:   "events",
				Usage:  "list incident timeline events",
				Action: incidentsEventsCmd,
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "page",
						Usage: "page number",
						Value: 1,
					},
					&cli.IntFlag{
						Name:  "per-page",
						Usage: "results per page",
						Value: 20,
					},
					&cli.StringFlag{
						Name:  "types",
						Usage: "comma-separated event types to filter, e.g. note,chat_message,role_update (the event/ prefix is optional; an unknown type is an error that lists the valid ones)",
					},
				},
			},
			{
				Name:   "transcript",
				Usage:  "list incident call transcript entries",
				Action: incidentsTranscriptCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "sort",
						Usage: "sort order: asc or desc",
						Value: "asc",
					},
				},
			},
			{
				Name:   "update",
				Usage:  "update incident fields",
				Action: incidentsUpdateCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "severity",
						Usage: "incident severity (e.g. SEV1)",
					},
					&cli.StringFlag{
						Name:  "summary",
						Usage: "incident summary text" + textFlagUsage,
					},
					&cli.StringFlag{
						Name:  "priority",
						Usage: "incident priority (e.g. P1)",
					},
					&cli.StringFlag{
						Name:  "description",
						Usage: "incident description" + textFlagUsage,
					},
				},
			},
			{
				Name:      "status-update",
				Usage:     "post an update to the incident's status pages, optionally moving its milestone",
				ArgsUsage: "<incident id or number>",
				Description: "Posts --note to the status pages attached to the incident. Choose where it goes:\n" +
					"--page (repeatable: a page's name, URL or ID, as `incidents status-pages` lists them),\n" +
					"--all-pages, or --internal-only (FireHydrant's internal status page only).\n" +
					"Pages are attached with `fhcli api incidents create-incident-status-page`.\n" +
					"Use --dry-run to see the request and the pages it targets without sending it.",
				Action: incidentsStatusUpdateCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "note",
						Aliases:  []string{"body", "b"},
						Usage:    "update text" + textFlagUsage,
						Required: true,
					},
					&cli.StringFlag{
						Name:  "milestone",
						Usage: "milestone slug to move the incident to (e.g. identified, mitigated; valid slugs: fhcli api incident-settings list-lifecycle-phases)",
					},
					&cli.StringSliceFlag{
						Name:  "page",
						Usage: "status page to post to: its name, URL or ID (repeatable)",
					},
					&cli.BoolFlag{
						Name:  "all-pages",
						Usage: "post to every status page attached to the incident",
					},
					&cli.BoolFlag{
						Name:  "internal-only",
						Usage: "post to FireHydrant's internal status page only",
					},
					&cli.BoolFlag{
						Name:  "no-email",
						Usage: "don't email the public pages' subscribers (they are emailed by default)",
					},
					&cli.BoolFlag{
						Name:  "dry-run",
						Usage: "print the request and the pages it targets instead of sending it",
					},
				},
			},
			{
				Name:      "status-pages",
				Usage:     "list the status pages attached to an incident",
				ArgsUsage: "<incident id or number>",
				Action:    incidentsStatusPagesCmd,
			},
			{
				Name:   "resolve",
				Usage:  "resolve an incident",
				Action: incidentsResolveCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "milestone",
						Usage: "post-incident milestone slug to set",
					},
				},
			},
			{
				Name:   "triage",
				Usage:  "get incident details, alerts, and summaries in one call",
				Action: incidentsTriageCmd,
			},
			{
				Name:   "context",
				Usage:  "get incident with enriched service and team details",
				Action: incidentsContextCmd,
			},
		},
	}
}

func incidentsListCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	page := int(cmd.Int("page"))
	perPage := int(cmd.Int("per-page"))

	req := operations.ListIncidentsRequest{
		Page:    firehydrantgosdk.Int(page),
		PerPage: firehydrantgosdk.Int(perPage),
	}

	if q := cmd.String("query"); q != "" {
		req.Query = firehydrantgosdk.String(q)
	}
	if s := cmd.String("status"); s != "" {
		req.Status = firehydrantgosdk.String(s)
	}
	if s := cmd.String("severity"); s != "" {
		req.Severities = firehydrantgosdk.String(s)
	}

	resp, err := client.Incidents.ListIncidents(ctx, req)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func incidentsGetCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID := incidentArg(cmd)

	resp, err := client.Incidents.GetIncident(ctx, incidentID)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func incidentsNoteCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}
	visibility, err := noteVisibility(cmd.String("visibility"))
	if err != nil {
		return failCmd(cmd, err)
	}
	body, err := textFlag(cmd, "body", new(bool))
	if err != nil {
		return failCmd(cmd, err)
	}

	resp, err := client.Incidents.CreateIncidentNote(ctx, incidentID, components.CreateIncidentNote{
		Body:       body,
		Visibility: visibility.ToPointer(),
	})
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func incidentsChatCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}
	body, err := textFlag(cmd, "body", new(bool))
	if err != nil {
		return failCmd(cmd, err)
	}

	resp, err := client.Incidents.CreateIncidentChatMessage(ctx, incidentID, components.CreateIncidentChatMessage{
		Body: body,
	})
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func incidentsAlertsCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}

	resp, err := client.Alerts.ListIncidentAlerts(ctx, incidentID)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func incidentsSummaryCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getRawClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}

	resp, err := listAudienceSummaries(ctx, client, incidentID)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

// listAudienceSummaries bypasses the SDK: the published spec documents this
// endpoint as {incident_id, audiences: string}, but it actually returns
// {data: {incident_id, summaries: [...]}}, so the SDK type decodes to {}.
func listAudienceSummaries(ctx context.Context, client *rawClient, incidentID string) (json.RawMessage, error) {
	return client.get(ctx, "/v1/audiences/summaries/"+url.PathEscape(incidentID), nil)
}

func incidentsEventsCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}

	page := int(cmd.Int("page"))
	perPage := int(cmd.Int("per-page"))

	var types *string
	if t := cmd.String("types"); t != "" {
		normalized, err := normalizeEventTypes(t)
		if err != nil {
			return failCmd(cmd, err)
		}
		types = firehydrantgosdk.String(normalized)
	}

	resp, err := client.Incidents.ListIncidentEvents(ctx, incidentID, types, firehydrantgosdk.Int(page), firehydrantgosdk.Int(perPage))
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

// incidentsTranscriptCmd bypasses the SDK: the published spec documents the
// list endpoint as returning a single transcript entry, but it returns a
// paginated {data: [...]} list, so the SDK type decodes to {}.
func incidentsTranscriptCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getRawClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}

	query := url.Values{}
	if s := cmd.String("sort"); s != "" {
		query.Set("sort", s)
	}

	resp, err := client.get(ctx, "/v1/incidents/"+url.PathEscape(incidentID)+"/transcript", query)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func incidentsUpdateCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}

	update := components.UpdateIncident{}

	if s := cmd.String("severity"); s != "" {
		update.Severity = firehydrantgosdk.String(s)
	}
	if s := cmd.String("priority"); s != "" {
		update.Priority = firehydrantgosdk.String(s)
	}
	stdinUsed := false
	for _, f := range []struct {
		name string
		dst  **string
	}{{"summary", &update.Summary}, {"description", &update.Description}} {
		v, err := textFlag(cmd, f.name, &stdinUsed)
		if err != nil {
			return failCmd(cmd, err)
		}
		if v != "" {
			*f.dst = firehydrantgosdk.String(v)
		}
	}

	resp, err := client.Incidents.UpdateIncident(ctx, incidentID, update)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func incidentsResolveCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}

	var reqBody *operations.ResolveIncidentRequestBody
	if m := cmd.String("milestone"); m != "" {
		reqBody = &operations.ResolveIncidentRequestBody{
			Milestone: firehydrantgosdk.String(m),
		}
	}

	resp, err := client.Incidents.ResolveIncident(ctx, incidentID, reqBody)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func incidentsTriageCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}
	raw, err := getRawClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID, err := resolveIncidentArg(ctx, cmd)
	if err != nil {
		return failCmd(cmd, err)
	}

	var (
		mu     sync.Mutex
		errs   []error
		result triageResult
		wg     sync.WaitGroup
	)

	wg.Add(3)

	go func() {
		defer wg.Done()
		resp, err := client.Incidents.GetIncident(ctx, incidentID)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errs = append(errs, fmt.Errorf("get incident: %w", err))
			return
		}
		result.Incident = resp
	}()

	go func() {
		defer wg.Done()
		resp, err := client.Alerts.ListIncidentAlerts(ctx, incidentID)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errs = append(errs, fmt.Errorf("list alerts: %w", err))
			return
		}
		result.Alerts = resp
	}()

	go func() {
		defer wg.Done()
		resp, err := listAudienceSummaries(ctx, raw, incidentID)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errs = append(errs, fmt.Errorf("list summaries: %w", err))
			return
		}
		result.Summaries = resp
	}()

	wg.Wait()

	if len(errs) > 0 {
		return failCmd(cmd, errors.Join(errs...))
	}

	return outputResult(cmd, result)
}

func incidentsContextCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("incident ID is required"))
	}
	incidentID := incidentArg(cmd)

	// Phase 1: Get the incident
	incident, err := client.Incidents.GetIncident(ctx, incidentID)
	if err != nil {
		return failCmd(cmd, fmt.Errorf("get incident: %w", err))
	}

	result := contextResult{
		Incident: incident,
	}

	// Phase 2: Fetch full details for each service in parallel
	type serviceResult struct {
		service *components.ServiceEntity
		ownerID string
	}
	serviceResults := make([]serviceResult, len(incident.Services))

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)

	for i, svc := range incident.Services {
		if svc.ID == nil {
			continue
		}
		wg.Add(1)
		go func(idx int, serviceID string) {
			defer wg.Done()
			resp, err := client.CatalogEntries.GetService(ctx, serviceID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("get service %s: %w", serviceID, err))
				return
			}
			sr := serviceResult{service: resp}
			if resp.Owner != nil && resp.Owner.ID != nil {
				sr.ownerID = *resp.Owner.ID
			}
			serviceResults[idx] = sr
		}(i, *svc.ID)
	}

	wg.Wait()
	if len(errs) > 0 {
		return failCmd(cmd, errors.Join(errs...))
	}

	// Build enriched services and collect unique team IDs
	teamIDs := make(map[string]struct{})
	for _, sr := range serviceResults {
		if sr.service == nil {
			continue
		}
		es := enrichedService{
			Service: sr.service,
		}
		for _, link := range sr.service.Links {
			name := ""
			url := ""
			if link.Name != nil {
				name = *link.Name
			}
			if link.HrefURL != nil {
				url = *link.HrefURL
			}
			if url != "" {
				es.DashboardLinks = append(es.DashboardLinks, serviceLink{Name: name, URL: url})
			}
		}
		if es.DashboardLinks == nil {
			es.DashboardLinks = []serviceLink{}
		}
		result.Services = append(result.Services, es)
		if sr.ownerID != "" {
			teamIDs[sr.ownerID] = struct{}{}
		}
	}
	if result.Services == nil {
		result.Services = []enrichedService{}
	}

	// Phase 3: Fetch team details and on-call schedules in parallel per unique team
	type teamResult struct {
		team   any
		oncall any
	}
	teamResults := make(map[string]teamResult)
	var mu2 sync.Mutex

	errs = nil
	for id := range teamIDs {
		wg.Add(1)
		go func(teamID string) {
			defer wg.Done()
			var tr teamResult
			var teamErr, oncallErr error

			// Two parallel calls per team
			var innerWg sync.WaitGroup
			innerWg.Add(2)

			go func() {
				defer innerWg.Done()
				resp, err := client.Teams.GetTeam(ctx, teamID, nil)
				if err != nil {
					teamErr = fmt.Errorf("get team %s: %w", teamID, err)
					return
				}
				tr.team = resp
			}()

			go func() {
				defer innerWg.Done()
				tid := teamID
				resp, err := client.Signals.ListOrganizationOnCallSchedules(ctx, &tid)
				if err != nil {
					oncallErr = fmt.Errorf("get on-call for team %s: %w", teamID, err)
					return
				}
				tr.oncall = resp
			}()

			innerWg.Wait()

			mu2.Lock()
			defer mu2.Unlock()
			if teamErr != nil {
				errs = append(errs, teamErr)
				return
			}
			if oncallErr != nil {
				errs = append(errs, oncallErr)
				return
			}
			teamResults[teamID] = tr
		}(id)
	}

	wg.Wait()
	if len(errs) > 0 {
		return failCmd(cmd, errors.Join(errs...))
	}

	for _, tr := range teamResults {
		result.Teams = append(result.Teams, enrichedTeam{
			Team:            tr.team,
			OnCallSchedules: tr.oncall,
		})
	}
	if result.Teams == nil {
		result.Teams = []enrichedTeam{}
	}

	return outputResult(cmd, result)
}

// incidentArg is the incident ID argument with any "#" stripped (an
// unstripped "#" would become a URL fragment and drop the ID). Only
// GET /v1/incidents/{id} accepts a number as-is; every other incident
// command uses resolveIncidentArg.
func incidentArg(cmd *cli.Command) string {
	return strings.TrimPrefix(cmd.Args().First(), "#")
}

// resolveIncidentArg is the incident ID argument, resolving an incident
// number ("623" or "#623") to its ID the way `fhcli api` does: some
// endpoints silently return nothing for a raw number.
func resolveIncidentArg(ctx context.Context, cmd *cli.Command) (string, error) {
	arg := cmd.Args().First()
	number, ok := incidentNumber(arg)
	if !ok {
		return incidentArg(cmd), nil
	}
	client, err := getRawClient(ctx)
	if err != nil {
		return "", err
	}
	return resolveIncidentNumber(ctx, client, number)
}
