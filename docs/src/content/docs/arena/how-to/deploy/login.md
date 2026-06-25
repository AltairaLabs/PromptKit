---
title: 'Deploy: Log In'
---

## Goal

Authenticate in your browser and let the adapter write the deploy config for you,
instead of manually exporting a profile and running `deploy config import`.

## Prerequisites

- An adapter installed whose provider supports login. Check with
  [Install Adapters](/arena/how-to/deploy/install-adapters/); a provider that
  supports login advertises the `login` capability.
- A local browser (this flow does not work headless — see [CI](#ci--headless)).
- Any non-secret coordinates the provider needs already set in your `arena.yaml`
  deploy section. For Omnia, that is `api_endpoint` — login fills in the rest.

## Log in

```bash
promptarena deploy login --provider omnia
```

Omit `--provider` if your config already has a `deploy.provider`:

```bash
promptarena deploy login
```

What happens:

1. The CLI opens your browser to the provider's authorize page.
2. You authenticate. The **provider brokers its own login** (OIDC or otherwise) —
   the CLI never sees your identity provider, so any IdP works.
3. You select the target workspace in the provider's UI.
4. The browser redirects back to the CLI, which writes the config and stores the
   token.

## What gets written

- **`arena.yaml`** — the deploy profile is merged under `deploy.config`:
  endpoint, workspace, providers, and skills. **No secret is written here.**
- **`~/.promptarena/credentials`** (mode `0600`) — the scoped token, keyed to this
  config file.

## Then deploy

```bash
promptarena deploy plan
promptarena deploy
```

The stored token is loaded automatically. Token precedence is: an explicit
`api_token` in the config **>** the provider's environment variable **>** the
credentials store.

## CI / headless

`deploy login` needs a local browser, so don't run it in CI. Instead, commit the
non-secret deploy config and supply the token through the provider's environment
variable (for Omnia, `OMNIA_API_TOKEN`). See [CI/CD Integration](/arena/how-to/deploy/ci-cd/).

## Troubleshooting

| Symptom | Cause / fix |
|---------|-------------|
| `adapter "<p>" does not support login` | The adapter has no `login` capability. Configure manually (see [Configure Deploy](/arena/how-to/deploy/configure/)) or install a newer adapter. |
| The browser didn't open | The CLI also prints the URL — open it manually. |
| `login timed out` | The CLI waits a few minutes for the callback. Re-run and finish in the browser. |

## See Also

- [Configure Deploy](/arena/how-to/deploy/configure/) — set the deploy section up manually
- [Plan and Apply](/arena/how-to/deploy/plan-and-apply/) — deploy once configured
- [CLI Commands](/arena/reference/deploy/cli-commands/) — full `deploy login` reference
