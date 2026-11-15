package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type jiraUser struct {
	AccountID    string `json:"accountId,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Active       bool   `json:"active,omitempty"`
}

type jiraPriority struct {
	Name string `json:"name"`
}

type jiraIssueType struct {
	Name    string `json:"name"`
	Subtask bool   `json:"subtask,omitempty"`
}

type jiraStatusCategory struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type jiraStatus struct {
	ID             string             `json:"id,omitempty"`
	Name           string             `json:"name"`
	StatusCategory jiraStatusCategory `json:"statusCategory"`
}

type jiraParent struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string      `json:"summary"`
		Status  *jiraStatus `json:"status"`
	} `json:"fields"`
}

type jiraComment struct {
	ID      string    `json:"id"`
	Body    any       `json:"body"`
	Author  *jiraUser `json:"author"`
	Created string    `json:"created"`
	Updated string    `json:"updated"`
}

type jiraIssue struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary     string         `json:"summary"`
		Status      *jiraStatus    `json:"status"`
		Assignee    *jiraUser      `json:"assignee"`
		Reporter    *jiraUser      `json:"reporter"`
		Priority    *jiraPriority  `json:"priority"`
		IssueType   *jiraIssueType `json:"issuetype"`
		Description any            `json:"description"`
		Labels      []string       `json:"labels"`
		Parent      *jiraParent    `json:"parent"`
		Created     string         `json:"created"`
		Updated     string         `json:"updated"`
		Comment     *struct {
			Comments []jiraComment `json:"comments"`
			Total    int           `json:"total"`
		} `json:"comment"`
	} `json:"fields"`
}

type searchRequest struct {
	JQL           string
	Fields        []string
	Limit         int
	NextPageToken string
}

type searchResult struct {
	Issues        []jiraIssue
	NextPageToken string
}

var defaultSearchFields = []string{"summary", "status", "assignee", "priority", "issuetype", "updated"}

func clampLimit(limit, def, max int) int {
	if limit <= 0 {
		limit = def
	}
	if limit > max {
		limit = max
	}
	return limit
}

func issueURL(c *Client, key string) string {
	return c.baseURL + "/browse/" + url.PathEscape(key)
}

func userAccount(user *jiraUser) string {
	if user == nil {
		return ""
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	return user.AccountID
}

func statusName(status *jiraStatus) string {
	if status == nil {
		return ""
	}
	return status.Name
}

func priorityName(priority *jiraPriority) string {
	if priority == nil {
		return ""
	}
	return priority.Name
}

func searchIssues(ctx context.Context, c *Client, settings Settings, req searchRequest) (*searchResult, error) {
	jql := strings.TrimSpace(req.JQL)
	if jql == "" {
		jql = defaultJQL(settings)
	}
	if jql == "" {
		return nil, fmt.Errorf("jql is required; pass jql or set PEGG_JIRA_JQL / default_jql")
	}
	limit := clampLimit(req.Limit, 50, 100)
	fields := req.Fields
	if len(fields) == 0 {
		fields = defaultSearchFields
	}
	payload := map[string]any{
		"jql":        jql,
		"maxResults": limit,
		"fields":     fields,
	}
	if req.NextPageToken != "" {
		payload["nextPageToken"] = req.NextPageToken
	}
	var response struct {
		Issues        []jiraIssue `json:"issues"`
		NextPageToken string      `json:"nextPageToken"`
		IsLast        bool        `json:"isLast"`
	}
	err := c.postJSON(ctx, c.api("/search/jql"), payload, &response)
	if err != nil {
		if !isHTTPStatus(err, http.StatusNotFound, http.StatusGone) {
			return nil, err
		}
		query := url.Values{}
		query.Set("jql", jql)
		query.Set("maxResults", fmt.Sprintf("%d", limit))
		query.Set("fields", strings.Join(fields, ","))
		response = struct {
			Issues        []jiraIssue `json:"issues"`
			NextPageToken string      `json:"nextPageToken"`
			IsLast        bool        `json:"isLast"`
		}{}
		var legacy struct {
			Issues []jiraIssue `json:"issues"`
			Total  int         `json:"total"`
		}
		if legacyErr := c.getJSON(ctx, c.api("/search"), query, &legacy); legacyErr != nil {
			return nil, legacyErr
		}
		response.Issues = legacy.Issues
	}
	if response.Issues == nil {
		response.Issues = []jiraIssue{}
	}
	return &searchResult{Issues: response.Issues, NextPageToken: response.NextPageToken}, nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

type transition struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	To   *jiraStatus `json:"to"`
}

func fetchTransitions(ctx context.Context, c *Client, key string) ([]transition, error) {
	var response struct {
		Transitions []transition `json:"transitions"`
	}
	if err := c.getJSON(ctx, c.api("/issue/"+url.PathEscape(key)+"/transitions"), nil, &response); err != nil {
		return nil, err
	}
	return response.Transitions, nil
}

func matchTransition(transitions []transition, target string) (*transition, []string) {
	target = strings.ToLower(strings.TrimSpace(target))
	names := make([]string, 0, len(transitions))
	for i := range transitions {
		names = append(names, transitions[i].Name)
		if target == "" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(transitions[i].Name)) == target ||
			strings.ToLower(strings.TrimSpace(transitions[i].ID)) == target {
			return &transitions[i], names
		}
	}
	return nil, names
}

func performTransition(ctx context.Context, c *Client, key, status string) (string, error) {
	transitions, err := fetchTransitions(ctx, c, key)
	if err != nil {
		return "", err
	}
	matched, names := matchTransition(transitions, status)
	if matched == nil {
		return "", fmt.Errorf("no transition matching %q; available transitions: %s", status, strings.Join(names, ", "))
	}
	payload := map[string]any{"transition": map[string]any{"id": matched.ID}, "fields": map[string]any{}}
	if err := c.postJSON(ctx, c.api("/issue/"+url.PathEscape(key)+"/transitions"), payload, nil); err != nil {
		return "", err
	}
	return matched.ID, nil
}

func addComment(ctx context.Context, c *Client, key, body string) error {
	payload := map[string]any{"body": markdownToADF(body)}
	return c.postJSON(ctx, c.api("/issue/"+url.PathEscape(key)+"/comment"), payload, nil)
}
