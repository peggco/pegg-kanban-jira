package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	peggplugin "github.com/peggco/pegg/pkg/plugin"
)

const (
	hookProviders     = "kanban.providers"
	hookIssueFetch    = "kanban.issues.fetch"
	hookCardSucceeded = "kanban.card.succeeded"

	hookPriority = 10
)

type ProviderInfo struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Enabled       bool   `json:"enabled"`
	Available     bool   `json:"available"`
	Authenticated bool   `json:"authenticated"`
	Detail        string `json:"detail,omitempty"`
	Target        string `json:"target,omitempty"`
	SyncMinutes   int    `json:"sync_minutes,omitempty"`
}

type HookIssue struct {
	ExternalID string   `json:"external_id,omitempty"`
	Number     int      `json:"number,omitempty"`
	Key        string   `json:"key,omitempty"`
	Title      string   `json:"title"`
	Body       string   `json:"body,omitempty"`
	URL        string   `json:"url,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	Assignees  []string `json:"assignees,omitempty"`
	State      string   `json:"state,omitempty"`
	Priority   string   `json:"priority,omitempty"`
	CreatedAt  string   `json:"created_at,omitempty"`
	UpdatedAt  string   `json:"updated_at,omitempty"`
}

type IssueFetch struct {
	Provider string      `json:"provider"`
	Target   string      `json:"target,omitempty"`
	Labels   []string    `json:"labels,omitempty"`
	State    string      `json:"state,omitempty"`
	Limit    int         `json:"limit,omitempty"`
	Query    string      `json:"query,omitempty"`
	Issues   []HookIssue `json:"issues"`
}

type IssueRef struct {
	Provider string `json:"provider,omitempty"`
	Repo     string `json:"repo"`
	Number   int    `json:"number"`
	Key      string `json:"key,omitempty"`
	URL      string `json:"url"`
}

type WorktreeEntry struct {
	Slug   string `json:"slug"`
	Path   string `json:"path"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Base   string `json:"base"`
	CardID string `json:"card_id"`
}

type Card struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	Column    string    `json:"column"`
	Priority  string    `json:"priority,omitempty"`
	Labels    []string  `json:"labels,omitempty"`
	Issue     *IssueRef `json:"issue,omitempty"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
}

type CardSucceeded struct {
	Card                  Card           `json:"card"`
	Worktree              *WorktreeEntry `json:"worktree,omitempty"`
	Summary               string         `json:"summary,omitempty"`
	Feedback              bool           `json:"feedback"`
	CreateChangeRequest   bool           `json:"create_change_request"`
	ChangeRequestProvider string         `json:"change_request_provider,omitempty"`
	TransitionOnSuccess   string         `json:"transition_on_success,omitempty"`
	BaseBranch            string         `json:"base_branch,omitempty"`
}

type cardSucceededResult struct {
	Key          string `json:"key"`
	Commented    bool   `json:"commented"`
	Transitioned bool   `json:"transitioned"`
}

func hookSpecs() []peggplugin.HookSpec {
	return []peggplugin.HookSpec{
		{
			Name:        hookProviders,
			Kind:        peggplugin.HookFilter,
			Priority:    hookPriority,
			Description: "Report Jira as a Kanban provider",
		},
		{
			Name:        hookIssueFetch,
			Kind:        peggplugin.HookFilter,
			Priority:    hookPriority,
			Description: "Fetch Jira issues for Kanban import",
		},
		{
			Name:        hookCardSucceeded,
			Kind:        peggplugin.HookAction,
			Priority:    hookPriority,
			Description: "Comment and transition the Jira issue when a Kanban card succeeds",
		},
	}
}

func (p *Plugin) Hooks() ([]peggplugin.HookSpec, error) {
	return hookSpecs(), nil
}

func (p *Plugin) ExecuteHook(req peggplugin.HookRequest) (*peggplugin.HookResult, error) {
	settings := p.currentSettings()
	if !settings.Enabled {
		return &peggplugin.HookResult{Handled: false}, nil
	}
	timeout := settings.timeout()
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	value, handled, err := p.executeHook(ctx, settings, req)
	if err != nil {
		return nil, fmt.Errorf("%s", redactSettings(err.Error(), settings))
	}
	return &peggplugin.HookResult{Value: value, Handled: handled}, nil
}

func (p *Plugin) executeHook(ctx context.Context, settings Settings, req peggplugin.HookRequest) (json.RawMessage, bool, error) {
	switch req.Name {
	case hookProviders:
		return p.providersHook(settings, req)
	case hookIssueFetch:
		var payload IssueFetch
		if err := decodeHookValue(req.Value, &payload); err != nil {
			return nil, false, err
		}
		if strings.TrimSpace(payload.Provider) != pluginName {
			return nil, false, nil
		}
		return p.fetchIssuesHook(ctx, settings, payload)
	case hookCardSucceeded:
		var payload CardSucceeded
		if err := decodeHookValue(req.Value, &payload); err != nil {
			return nil, false, err
		}
		if payload.Card.Issue == nil || strings.TrimSpace(payload.Card.Issue.Provider) != pluginName {
			return nil, false, nil
		}
		return p.cardSucceededHook(ctx, settings, payload)
	default:
		return nil, false, nil
	}
}

func decodeHookValue(raw json.RawMessage, out any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s payload: %w", pluginName, err)
	}
	return nil
}

func (p *Plugin) providersHook(settings Settings, req peggplugin.HookRequest) (json.RawMessage, bool, error) {
	var providers []ProviderInfo
	if err := decodeHookValue(req.Value, &providers); err != nil {
		return nil, false, err
	}
	if !settings.Enabled {
		return nil, false, nil
	}
	info := providerInfo(settings)
	replaced := false
	for i := range providers {
		if providers[i].ID == pluginName {
			providers[i] = info
			replaced = true
			break
		}
	}
	if !replaced {
		providers = append(providers, info)
	}
	value, err := json.Marshal(providers)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func providerInfo(settings Settings) ProviderInfo {
	authenticated := settings.authenticated()
	target := strings.TrimSpace(settings.Project)
	available := settings.Enabled && authenticated
	return ProviderInfo{
		ID:            pluginName,
		Label:         pluginDisplay,
		Enabled:       settings.Enabled,
		Available:     available,
		Authenticated: authenticated,
		Detail:        providerDetail(settings, authenticated, target),
		Target:        target,
	}
}

func providerDetail(settings Settings, authenticated bool, target string) string {
	switch {
	case !settings.Enabled:
		return "Plugin disabled; enable Jira to sync issues"
	case !authenticated:
		if settings.isDataCenter() {
			return "Set base_url and a personal access token, or username and password, to authenticate"
		}
		return "Set site, email, and API token to authenticate"
	case target == "":
		return "Set a project key to select issues"
	default:
		return "Ready to sync from " + target
	}
}

func (p *Plugin) fetchIssuesHook(ctx context.Context, settings Settings, payload IssueFetch) (json.RawMessage, bool, error) {
	client := newClient(settings)
	query := strings.TrimSpace(payload.Query)
	if query == "" {
		query = defaultJQL(settings)
	}
	limit := payload.Limit
	if limit <= 0 {
		limit = settings.SyncLimit
	}
	limit = clampLimit(limit, 30, 100)
	result, err := searchIssues(ctx, client, settings, searchRequest{
		JQL:    query,
		Limit:  limit,
		Fields: []string{"summary", "description", "status", "assignee", "priority", "issuetype", "created", "updated", "labels"},
	})
	if err != nil {
		return nil, false, err
	}
	for _, issue := range result.Issues {
		payload.Issues = append(payload.Issues, hookIssue(client, issue))
	}
	if payload.Issues == nil {
		payload.Issues = []HookIssue{}
	}
	value, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func hookIssue(client *Client, issue jiraIssue) HookIssue {
	assignees := []string{}
	if assignee := userAccount(issue.Fields.Assignee); assignee != "" {
		assignees = append(assignees, assignee)
	}
	externalID := strings.TrimSpace(issue.ID)
	if externalID == "" {
		externalID = issue.Key
	}
	return HookIssue{
		ExternalID: externalID,
		Key:        issue.Key,
		Title:      issue.Fields.Summary,
		Body:       adfToMarkdown(issue.Fields.Description),
		URL:        issueURL(client, issue.Key),
		Labels:     nonNilStrings(issue.Fields.Labels),
		Assignees:  assignees,
		State:      statusName(issue.Fields.Status),
		Priority:   priorityName(issue.Fields.Priority),
		CreatedAt:  issue.Fields.Created,
		UpdatedAt:  issue.Fields.Updated,
	}
}

func (p *Plugin) cardSucceededHook(ctx context.Context, settings Settings, payload CardSucceeded) (json.RawMessage, bool, error) {
	issue := payload.Card.Issue
	key := strings.TrimSpace(issue.Key)
	if key == "" {
		return nil, false, nil
	}
	client := newClient(settings)
	result := cardSucceededResult{Key: key}
	if payload.Feedback && strings.TrimSpace(payload.Summary) != "" {
		if err := addComment(ctx, client, key, payload.Summary); err != nil {
			return nil, false, err
		}
		result.Commented = true
	}
	if status := strings.TrimSpace(payload.TransitionOnSuccess); status != "" {
		if _, err := performTransition(ctx, client, key, status); err != nil {
			return nil, false, err
		}
		result.Transitioned = true
	}
	value, err := json.Marshal(result)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}
