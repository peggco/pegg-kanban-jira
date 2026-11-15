package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	configKey    = "kanban-jira"
	configKeyAlt = "pegg-kanban-jira"
)

var configKeys = []string{"jira", "pegg-jira", configKey, configKeyAlt}

type Settings struct {
	Enabled               bool     `json:"enabled"`
	Site                  string   `json:"site"`
	Email                 string   `json:"email"`
	Token                 string   `json:"token"`
	Project               string   `json:"project"`
	DefaultJQL            string   `json:"default_jql"`
	SyncState             string   `json:"sync_state"`
	SyncLabels            []string `json:"sync_labels"`
	SyncLimit             int      `json:"sync_limit"`
	DefaultTransition     string   `json:"default_transition"`
	RequestTimeoutSeconds int      `json:"request_timeout_seconds"`
	Workdir               string   `json:"-"`
}

func defaultSettings() Settings {
	wd, _ := os.Getwd()
	return Settings{
		Enabled:               true,
		SyncLimit:             30,
		RequestTimeoutSeconds: 30,
		Workdir:               wd,
	}
}

func (s *Settings) applyHost(configJSON []byte) {
	if len(configJSON) == 0 {
		return
	}
	var host struct {
		Plugins struct {
			Options map[string]json.RawMessage `json:"options"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(configJSON, &host); err != nil {
		return
	}
	for _, key := range configKeys {
		raw := host.Plugins.Options[key]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		_ = json.Unmarshal(raw, s)
	}
}

func (s *Settings) applyOptions(options json.RawMessage) error {
	if len(options) == 0 || string(options) == "null" {
		return nil
	}
	if err := json.Unmarshal(options, s); err != nil {
		return fmt.Errorf("parse %s options: %w", configKey, err)
	}
	return nil
}

func (s *Settings) applyEnv() {
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_OPTIONS")); v != "" {
		_ = s.applyOptions(json.RawMessage(v))
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_SITE")); v != "" {
		s.Site = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_EMAIL")); v != "" {
		s.Email = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_TOKEN")); v != "" {
		s.Token = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_PROJECT")); v != "" {
		s.Project = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_JQL")); v != "" {
		s.DefaultJQL = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_SYNC_STATE")); v != "" {
		s.SyncState = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_SYNC_LABELS")); v != "" {
		s.SyncLabels = parseStringList(v)
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_SYNC_LIMIT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.SyncLimit = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_DEFAULT_TRANSITION")); v != "" {
		s.DefaultTransition = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_TIMEOUT_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.RequestTimeoutSeconds = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_JIRA_DISABLED")); v == "1" || strings.EqualFold(v, "true") {
		s.Enabled = false
	}
	s.normalize()
}

func (s *Settings) normalize() {
	s.Site = strings.TrimSpace(s.Site)
	s.Email = strings.TrimSpace(s.Email)
	s.Token = strings.TrimSpace(s.Token)
	s.Project = strings.TrimSpace(s.Project)
	s.DefaultJQL = strings.TrimSpace(s.DefaultJQL)
	s.SyncState = strings.TrimSpace(s.SyncState)
	s.DefaultTransition = strings.TrimSpace(s.DefaultTransition)
	s.SyncLabels = cleanStrings(s.SyncLabels)
	if s.SyncLimit <= 0 {
		s.SyncLimit = 30
	}
	if s.RequestTimeoutSeconds <= 0 {
		s.RequestTimeoutSeconds = 30
	}
	if strings.TrimSpace(s.Workdir) == "" {
		s.Workdir, _ = os.Getwd()
	}
	s.Workdir = expandHome(s.Workdir)
}

func (s Settings) BaseURL() string {
	site := strings.TrimSpace(s.Site)
	if site == "" {
		return ""
	}
	site = strings.TrimRight(site, "/")
	if strings.HasPrefix(site, "http://") || strings.HasPrefix(site, "https://") {
		return site
	}
	if strings.Contains(site, ".") {
		return "https://" + site
	}
	return "https://" + site + ".atlassian.net"
}

func (s Settings) timeout() time.Duration {
	if s.RequestTimeoutSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(s.RequestTimeoutSeconds) * time.Second
}

func (s Settings) configured() bool {
	return s.BaseURL() != "" && s.Email != "" && s.Token != ""
}

func (s Settings) authenticated() bool {
	return strings.TrimSpace(s.Email) != "" && strings.TrimSpace(s.Token) != ""
}

func (s Settings) workdir() string {
	if strings.TrimSpace(s.Workdir) != "" {
		return s.Workdir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func redactSettings(msg string, s Settings) string {
	for _, secret := range []string{s.Token, basicAuth(s.Email, s.Token)} {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "[redacted]")
		}
	}
	return msg
}

func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("~", ".pegg", "pegg.json")
	}
	return filepath.Join(home, ".pegg", "pegg.json")
}

func persistSettings(settings Settings) (string, error) {
	path := configPath()
	root := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
	} else if len(data) > 0 {
		if err := json.Unmarshal(data, &root); err != nil {
			return "", fmt.Errorf("parse %s: %w", path, err)
		}
	}

	plugins, ok := root["plugins"].(map[string]any)
	if !ok {
		plugins = map[string]any{}
		root["plugins"] = plugins
	}
	options, ok := plugins["options"].(map[string]any)
	if !ok {
		options = map[string]any{}
		plugins["options"] = options
	}
	key := configKey
	for _, candidate := range []string{configKey, configKeyAlt, "jira", "pegg-jira"} {
		if _, ok := options[candidate]; ok {
			key = candidate
			break
		}
	}
	entry, ok := options[key].(map[string]any)
	if !ok {
		entry = map[string]any{}
	}
	entry["enabled"] = settings.Enabled
	entry["site"] = settings.Site
	entry["email"] = settings.Email
	entry["token"] = settings.Token
	entry["project"] = settings.Project
	entry["default_jql"] = settings.DefaultJQL
	entry["sync_state"] = settings.SyncState
	entry["sync_labels"] = settings.SyncLabels
	entry["sync_limit"] = settings.SyncLimit
	entry["default_transition"] = settings.DefaultTransition
	entry["request_timeout_seconds"] = settings.RequestTimeoutSeconds
	options[key] = entry

	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode config: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pegg-plugin-*.json")
	if err != nil {
		return "", fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

func parseStringList(value string) []string {
	var values []string
	if err := json.Unmarshal([]byte(value), &values); err == nil {
		return values
	}
	return cleanStrings(strings.Split(value, ","))
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func defaultJQL(settings Settings) string {
	if strings.TrimSpace(settings.DefaultJQL) != "" {
		return settings.DefaultJQL
	}
	project := strings.TrimSpace(settings.Project)
	if project == "" {
		return ""
	}
	clauses := []string{"project = " + project}
	if state := strings.TrimSpace(settings.SyncState); state != "" {
		clauses = append(clauses, `status = "`+escapeJQL(state)+`"`)
	}
	for _, label := range settings.SyncLabels {
		if label = strings.TrimSpace(label); label != "" {
			clauses = append(clauses, `labels = "`+escapeJQL(label)+`"`)
		}
	}
	return strings.Join(clauses, " AND ") + " ORDER BY updated DESC"
}

func escapeJQL(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
