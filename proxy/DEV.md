# Running the editor login proxy locally

The dev stack is four containers: a Keycloak with its own realm, a one-shot
script that configures that realm, the proxy, and a stand-in `/admin` page so
the login handshake can be exercised before the real Decap configuration
exists.

Nothing here touches id.prodeko.org. The one real credential is a GitHub token.

## Bring it up

```bash
cp .env.example .env            # paste a GitHub token into GITHUB_TOKEN
DOCKER_BUILDKIT=0 docker compose up --build
```

Then open <http://localhost:1313/admin/> and sign in as
`editor@prodeko.org` / `kananugetti`.

Four addresses:

| What                  | Where                                             |
| --------------------- | ------------------------------------------------- |
| Editing screen (stub) | <http://localhost:1313/admin/>                    |
| Proxy                 | <http://localhost:8080>                           |
| Keycloak              | <http://localhost:8180>                           |
| Keycloak admin console| <http://localhost:8180/admin> — `admin` / `admin` |

Editing requires both the `membership` and the `prodeko-org-admin` realm role,
and three users demonstrate what that means. Only `editor@prodeko.org`, who has
both, gets in. `member@prodeko.org` has `membership` alone: a perfectly valid
Prodeko account with no business editing the website. `lapsed@prodeko.org` has
`prodeko-org-admin` alone, which is what an editor looks like after their guild
membership runs out, and is refused for it — that is the whole reason both are
required. All three use the password `kananugetti`. If either of the last two
can edit, the first of the four rules is broken.

## `DOCKER_BUILDKIT=0`

On a rootless Docker over fuse-overlayfs, BuildKit cannot do a cross-stage
`COPY --from`; every attempt fails with `operation not supported`, which reads
like a bug in the Dockerfile and is not. The legacy builder works. The
Dockerfile is kept free of BuildKit-only syntax so it builds either way, at the
cost of a module cache mount and therefore slower rebuilds.

CI and production BuildKit are unaffected; the flag is a local workaround.

## The GitHub token

A fine-grained personal access token on a bot account, scoped to the single
repository in `.env`, with **Contents: read and write** and **Pull requests:
read and write**. Create one at
<https://github.com/settings/personal-access-tokens>.

Every write the proxy makes lands in the real repository named by
`GITHUB_OWNER`/`GITHUB_REPO`/`GITHUB_BRANCH`. Point it at a scratch repository
unless you mean it.

## Why Keycloak has two addresses

The browser reaches Keycloak at `localhost:8180`; the proxy, inside the compose
network, reaches it at `keycloak:8180`. A token carries exactly one `iss`, so
one of those has to be canonical.

`KC_HOSTNAME=http://localhost:8180` fixes the front channel — and therefore
`iss` and the login URL — on localhost, while
`KC_HOSTNAME_BACKCHANNEL_DYNAMIC=true` makes the discovery document advertise
`keycloak:8180` for the token and JWKS endpoints when it is fetched from inside
the network. The proxy follows suit: it discovers at `KEYCLOAK_DISCOVERY_URL`
and validates `iss` against `KEYCLOAK_ISSUER`.

`KEYCLOAK_DISCOVERY_URL` exists only for this. In production the two values are
the same and it is left unset; `config.SplitHorizon()` reports which case you
are in.

## Re-running the realm bootstrap

`dev/keycloak/setup.py` is idempotent — roles, the client, its protocol mapper
and the users converge on the same state however many times it runs.

```bash
docker compose run --rm kc-setup
```

Keycloak here runs `start-dev` with no database, so `docker compose down`
discards the realm entirely and the next `up` rebuilds it from scratch.

**Never run this against production Keycloak.** It refuses to start if
`KEYCLOAK_URL` mentions id.prodeko.org, but that is a guard rail, not a
permission slip. Production clients are registered by hand through the admin
console, following
[membership-registry/docs/keycloak-clients.md](https://github.com/prodeko/membership-registry/blob/main/docs/keycloak-clients.md).

## The protocol mapper that decides whether anyone can edit

Keycloak's built-in `roles` client scope puts `realm_access.roles` in the
access token only. The proxy prefers the ID token and falls back to the access
token, verifying it against the same JWKS and requiring `azp` to be our client,
so an untouched realm does work — but the ID token is the shorter path and the
one worth configuring. Which source was used is logged once per process. The
mapper to add:

- Type `User Realm Role`, name `realm roles`
- Multivalued: on
- Token Claim Name: `realm_access.roles`
- Add to ID token: on, add to access token: on

`setup.py` creates it here. Production is configured by hand and can easily
miss it, and the failure is quiet and misleading: the claim is simply absent,
the role set looks empty, and every editor is denied with a correct-looking
"not an editor" message. Check it under `Client scopes` → `Evaluate` on the
production client before blaming the proxy.

The failure mode is safe — nobody gets in who should not — so resist any fix
that loosens the role check. The mapper is the fix.

## What the stub `/admin` does and does not prove

`dev/admin/` is a minimal Decap page standing in for `site/static/admin/` until
the real configuration lands (roadmap item D). It proves the login handshake,
the session, and the GitHub forwarding. It does not prove that the real site's
collections save correctly, and the save path is exactly what the CivicDataLab
prior art never tested. Re-test against the real configuration the day it
exists.

Two things in `dev/admin/config.yml` have to stay in step with the rest:

- `repo:` and `branch:` must match `GITHUB_OWNER`/`GITHUB_REPO`/`GITHUB_BRANCH`
  in `.env`. The proxy pins the repository server-side either way, but a
  mismatch makes the editing screen describe a different repository from the
  one it writes to.
- `base_url:` must equal the proxy's `PUBLIC_URL` exactly, as a bare origin.
  Decap compares the login popup's `postMessage` event origin against this
  string, and an event origin is always `scheme://host[:port]` with no path. A
  trailing slash or a path segment makes login hang silently: the popup opens,
  closes, and the CMS never reacts. `config.Load` rejects a `PUBLIC_URL` that
  is not a bare origin for the same reason.

There is no `local_backend` key, on purpose. Decap's local backend bypasses the
proxy entirely, so testing with it exercises none of the login path while
appearing to work.

## Running the proxy outside Docker

```bash
docker compose up keycloak kc-setup admin     # dependencies only
set -a && source .env && set +a
PUBLIC_URL=http://localhost:8080 \
CMS_ORIGINS=http://localhost:1313 \
KEYCLOAK_ISSUER=http://localhost:8180/realms/membership-registry \
KEYCLOAK_CLIENT_ID=cms-auth-proxy \
KEYCLOAK_CLIENT_SECRET=dev-secret-cms-auth-proxy \
EDITOR_ROLES=membership,prodeko-org-admin \
SESSION_SECRET=dev-session-secret-not-for-production-use-0000000 \
LOG_LEVEL=debug \
go run ./cmd/proxy
```

`KEYCLOAK_DISCOVERY_URL` is not set here: on the host, `localhost:8180` is the
right address for both channels.

Faster to iterate on, but it stops exercising the container, which is how
container-only problems survive until the demo. Build the image before you
believe anything.

## Tests

```bash
go test ./...
go vet ./...
```

`internal/config` has no network dependency and no fixtures, so its tests run
in milliseconds. They cover the validation that carries security weight:
repository pinning (`GITHUB_OWNER`/`GITHUB_REPO` must be single path segments,
because they are substituted into a forwarded URL), the `PUBLIC_URL` origin
rule, session secret length, and that neither `String()` nor any error message
can leak a secret into a log or a pasted stack trace.

## Configuration

Every variable, with its validation rule, is documented in `.env.example`.
`config.Load` reports every problem it finds in one pass and refuses to return
a `Config` when anything is wrong, so a half-configured proxy never starts:

```
invalid configuration, 3 problems:
  - PUBLIC_URL: must be a bare origin with no path, got path "/cms" ...
  - GITHUB_OWNER: must be a single path segment matching ...
  - SESSION_SECRET: must be at least 32 bytes, got 11
```

The dev `SESSION_SECRET` is refused outright whenever `PUBLIC_URL` is not
localhost, so the placeholder in `compose.yaml` cannot quietly follow a
copy-paste into production. That is a tripwire for one known string, not a
check that your secret is any good.

## Deliberate gaps

The proxy serves `GET /healthz`, but the container has no `HEALTHCHECK`. The
final image is `gcr.io/distroless/static` — no shell, no curl, nothing to exec
— so a container-level check needs either a self-check mode in the binary or a
probe from outside, such as Kubernetes or a reverse proxy calling `/healthz`
over the network. Startup ordering in compose is gated on the realm bootstrap
finishing instead, which is the dependency that actually matters.
