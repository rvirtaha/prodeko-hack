# Editor login proxy

The service that lets Decap CMS use Prodeko accounts instead of GitHub
accounts. It is the only new backend component in the design.

Nothing is implemented yet. This file is the contract the implementation has to
meet, written down so the work can start without waiting on a design
discussion. The language and framework are still open.

## What it does

Decap runs entirely in the browser and normally talks to GitHub directly, which
would mean every editor needs a GitHub account with write access. Decap does
let you redirect both its login and its GitHub calls elsewhere, and it treats
the login token as an opaque string without checking where it came from. This
service sits in that gap.

```mermaid
sequenceDiagram
    participant B as Browser (Decap)
    participant P as Proxy
    participant K as Keycloak
    participant G as GitHub

    B->>P: GET /auth
    P->>K: sign in
    K-->>P: identity and roles
    P-->>B: session token via postMessage
    B->>P: GitHub API call with session token
    P->>P: check session, check role, pin repository
    P->>G: same call, GitHub credentials attached, author replaced
    G-->>B: response
```

## Endpoints

- `GET /auth` starts the sign-in. Opens the Keycloak login and remembers where
  to return.
- `GET /callback` finishes it. Verifies the Keycloak response, checks the
  editor role, creates a session, and hands the session token back to the
  opener window with `postMessage`.
- `ANY /github/*` forwards to the GitHub API. Everything under here requires a
  valid session.

## Rules the implementation must follow

- Require an editor role from the Keycloak token. Without this, anyone with a
  Prodeko account can edit the website.
- Replace the author on every write with the name and email from the Keycloak
  session. Never trust an author supplied by the browser.
- Pin the repository. The owner and repository name come from configuration and
  are substituted into the forwarded path, so a modified browser cannot write
  somewhere else.
- Keep the GitHub credentials server-side. The browser only ever sees the
  session token.
- Issue our own session token rather than passing the Keycloak token through.
  Decap does not renew an expired login, so the session outlives the Keycloak
  access token and is refreshed behind the scenes.
- Allow only the GitHub paths Decap actually uses. Repository contents, git
  objects, pull requests, and the current user. Not collaborators, webhooks or
  secrets.

## Configuration

Expected as environment variables:

- `KEYCLOAK_ISSUER`, `KEYCLOAK_CLIENT_ID`, `KEYCLOAK_CLIENT_SECRET`
- `EDITOR_ROLE`, the realm role required to edit
- `GITHUB_TOKEN`, a fine-grained token on a bot account, scoped to one
  repository with contents and pull request write access
- `GITHUB_OWNER`, `GITHUB_REPO`, `GITHUB_BRANCH`
- `SESSION_SECRET`
- `PUBLIC_URL`, the address this service is reachable at

## How the site points at it

In the Decap configuration under `site/`:

```yaml
backend:
  name: github
  repo: prodeko/prodeko-hack
  branch: main
  base_url: https://cms.prodeko.org
  auth_endpoint: auth
  api_root: https://cms.prodeko.org/github
```

The local development mode that Decap offers must stay switched off. It
bypasses this service entirely, so testing with it exercises none of the real
login path while appearing to work.

## Prior art

The same combination of Decap, Keycloak and a server-side GitHub credential has
been built before, in
[CivicDataLab pull request 316](https://github.com/CivicDataLab/civicdatalab.github.io/pull/316).
Worth reading before starting. Its author notes that a full save through the
real editing screen was never tested end to end, so that is the first thing to
prove here rather than the last.
