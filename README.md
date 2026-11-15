<div align="center">

<img src="https://cdn.pegg.dev/logo/infinity.gif" alt="Pegg" width="420" />

# Pegg Kanban Jira

### Jira issues on your Kanban board.

A Kanban integration for Jira. It imports issues and comments or transitions the issue when a card succeeds.

[Plugins docs](https://docs.pegg.dev/pegg/configurations/plugins/) · [Releases](https://github.com/peggco/pegg-kanban-jira/releases) · [Website](https://pegg.dev) · [Discussions](https://github.com/peggco/pegg/discussions)

[![Latest release](https://img.shields.io/github/v/release/peggco/pegg-kanban-jira?style=flat-square&sort=semver)](https://github.com/peggco/pegg-kanban-jira/releases)
[![Go 1.26.5+](https://img.shields.io/badge/go-1.26.5%2B-00ADD8?style=flat-square&logo=go&logoColor=white)](https://github.com/peggco/pegg-kanban-jira)
[![Plugin protocol v2](https://img.shields.io/badge/plugin%20protocol-v2-black?style=flat-square)](https://docs.pegg.dev/pegg/configurations/plugins/)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-black?style=flat-square)](https://github.com/peggco/pegg-kanban-jira/blob/HEAD/LICENSE)

**Install**

```bash
pegg plugins install peggco/pegg-kanban
pegg plugins install peggco/pegg-kanban-jira
```

Hooks-only. Install `pegg-kanban` alongside this plugin; it contributes no agent tools.
</div>

## Setup

1. Create a Jira Cloud API token at
   <https://id.atlassian.com/manage-profile/security/api-tokens>.
2. Add the plugin options to `~/.pegg/pegg.json`:

```json
{
  "plugins": {
    "options": {
      "kanban-jira": {
        "enabled": true,
        "site": "acme",
        "email": "you@example.com",
        "token": "ATATT...",
        "project": "ABC",
        "default_jql": "project = ABC AND status != Done ORDER BY updated DESC",
        "request_timeout_seconds": 30
      }
    }
  }
}
```

`site` accepts `acme`, `acme.atlassian.net`, or a full URL with or without a
scheme. The derived base URL is `https://<site>.atlassian.net` unless a full URL
is supplied.

The preferred option keys are `kanban-jira` and `pegg-kanban-jira`. The legacy
`jira` and `pegg-jira` keys are still read as fallbacks, with the preferred keys
taking precedence.

### Environment variables

| Variable | Meaning |
|---|---|
| `PEGG_JIRA_OPTIONS` | JSON options object |
| `PEGG_JIRA_SITE` | Jira site or full URL |
| `PEGG_JIRA_EMAIL` | Atlassian account email |
| `PEGG_JIRA_TOKEN` | API token |
| `PEGG_JIRA_PROJECT` | Default project key |
| `PEGG_JIRA_JQL` | Default JQL used when a fetch request has no query |
| `PEGG_JIRA_SYNC_STATE` | Default state filter appended to the project query |
| `PEGG_JIRA_SYNC_LABELS` | JSON array or comma-separated label filter |
| `PEGG_JIRA_SYNC_LIMIT` | Default fetch limit (default 30, capped at 100) |
| `PEGG_JIRA_DEFAULT_TRANSITION` | Fallback status transition when the card payload has none |
| `PEGG_JIRA_TIMEOUT_SECONDS` | Request timeout (default 30) |
| `PEGG_JIRA_DISABLED` | `1` or `true` disables the hooks |

Precedence: defaults → `~/.pegg/pegg.json` (`kanban-jira`, `pegg-kanban-jira`,
`jira`, or `pegg-jira`) → host plugin options → `PEGG_JIRA_*` environment
variables → normalization.

## Hooks

Hook specs are always registered. A disabled plugin, an unknown hook, or a
request for another provider returns `handled: false` without an error.

| Hook | Kind | Priority |
|---|---|---|
| `kanban.providers` | filter | 10 |
| `kanban.issues.fetch` | filter | 10 |
| `kanban.card.succeeded` | action | 10 |

### `kanban.providers`

Appends or replaces the Jira entry in the provider list:

```json
{
  "id": "jira",
  "label": "Jira",
  "enabled": true,
  "available": true,
  "authenticated": true,
  "detail": "Ready to sync from ABC",
  "target": "ABC",
  "sync_minutes": 0
}
```

`target` is the configured project key. `available` requires the plugin to be
enabled plus authenticated credentials (and is reported alongside the project
target). `sync_minutes` is owned by Kanban and emitted as `0`.

### `kanban.issues.fetch`

Accepts `{provider,target,labels,state,limit,query,issues}`. Only
`provider:"jira"` is handled. `query` overrides JQL; otherwise `default_jql`
is used, or a query is built from the project plus `sync_state` and
`sync_labels`. A missing request `limit` falls back to `sync_limit`. Matching
issues are appended to `issues` with:

```json
{
  "external_id": "10001",
  "key": "ABC-7",
  "title": "Fix login",
  "body": "Details",
  "url": "https://acme.atlassian.net/browse/ABC-7",
  "labels": ["api"],
  "assignees": ["Ada"],
  "state": "In Progress",
  "priority": "High",
  "created_at": "2024-01-01T00:00:00.000+0000",
  "updated_at": "2024-01-02T03:04:05.000+0000"
}
```

Descriptions are converted from Atlassian Document Format to Markdown. The
fetch limit is clamped to at most 100 issues per request.

### `kanban.card.succeeded`

Accepts
`{card,worktree,summary,feedback,create_change_request,transition_on_success,base_branch}`.
Only cards whose `card.issue.provider` is `jira` are handled.

- When `feedback` is true and `summary` is non-empty, the summary is posted as
  a Jira comment.
- When `transition_on_success` is non-empty, the issue is transitioned to that
  status. The value matches a transition **name** or **id** returned by Jira;
  if no transition matches, the hook returns an error listing the available
  transitions.
- The result is `{key,commented,transitioned}`.

The payload structs are mirrored locally in `hooks.go`; no Kanban code is
imported.

## Build from source

```sh
go vet ./...
go test ./...
go build -trimpath -o bin/pegg-kanban-jira .
install -m 0755 bin/pegg-kanban-jira "$HOME/.pegg/plugins/pegg-kanban-jira"
```

Then open the Kanban board (the `pegg-kanban` plugin) and select the Jira
provider.

## License

Apache-2.0. See [LICENSE](https://github.com/peggco/pegg-kanban-jira/blob/HEAD/LICENSE).
