# Keycloak SSO for prodeko.org and matrikkeli

Status: design agreed 2026-08-14. Implementation plan not yet written.

## Goal

Make Keycloak (`id.prodeko.org`, realm `membership-registry`) the sole authentication and
authorization authority for the prodeko.org Django project, which also serves matrikkeli. The
project becomes an OpenID Connect relying party. Its own OAuth2 provider is retired, and local
password authentication survives only as a single documented break-glass account for the case where
Keycloak is unreachable. Existing Django user rows are preserved and adopted by the matching
Keycloak identity on first login; rows with no Keycloak counterpart survive as data but can no
longer be used to sign in.

This is the last application still consuming prodeko.org's Django SSO.

## Current state

The facts below were verified against the repositories, not assumed.

prodeko.org is a single Django 5.2 project. It serves prodeko.org, matrikkeli at
`/fi/matrikkeli/`, tiedotteet, abit, lifelonglearning and seminaari from one codebase, one session
and one user model. `matrikkeli.prodeko.org` is a Caddy redirect, not a separate deployment.

- `AUTH_USER_MODEL` is `auth_prodeko.User`, with `email` as `USERNAME_FIELD` and `username = None`.
- `LOGIN_URL` is `auth_prodeko:login` (`prodekoorg/settings/base.py:48`). Login lives at
  `/fi/login/` with the template `prodeko_login.html`.
- Matrikkeli has a second, parallel login and password-reset chain
  (`alumnirekisteri/rekisteri/urls.py:224-258`). Its login view is already broken:
  `urls.py:10` calls `u.is_anonymous()` as a method, which raises `TypeError` on modern Django.
  Matrikkeli logins therefore already go through `/fi/login/`.
- 58 view decorators in matrikkeli and 12 in tiedotteet hardcode `login_url="/login/"`, a path that
  is not a URL pattern and only resolves through `LocaleMiddleware`'s 404 redirect.
- The legacy provider is django-oauth-toolkit plus `prodekoorg/app_oauth`, exposing
  `/oauth2/auth`, `/oauth2/token`, `/oauth2/revoke-token`, the application-management views and
  `/oauth2/user_details/`.
- `REST_FRAMEWORK.DEFAULT_AUTHENTICATION_CLASSES` includes `BasicAuthentication`
  (`settings/base.py:438`), so every DRF endpoint currently accepts an email and password over HTTP.
- Matrikkeli's admin UI writes `user.is_staff` directly (`alumnirekisteri/rekisteri/views.py:161`
  and `:230`). Because `is_staff` also gates Django CMS admin, a matrikkeli admin today has full
  site admin. Any staff user can promote any other user to staff.
- Matrikkeli uses `is_active=False` as its pending-approval queue (`views.py:212`) and as a
  visibility filter in search (`views.py:1896`) and CSV export (`views.py:326`).
- `app_membership` is live: two published CMS pages route to it, and its acceptance flow mints a
  random password and emails it in plaintext (`prodekoorg/app_membership/models.py:96-112`,
  templates `emails/accept_mail_*`).
- Keycloak is an Azure App Service defined in Terraform with
  `lifecycle { ignore_changes = [app_settings] }`. Realms, clients, redirect URIs and secrets are
  managed by hand in the Keycloak admin console. There is no realm-as-code.
- The membership registry exposes no API, webhook or event bus that another application can consume.
  Every route is behind an HttpOnly session cookie; bearer tokens are not accepted anywhere.

## Approach

### Library

Use `mozilla-django-oidc >= 5.0.0`, whose 5.0.0 release added Django 5.0 through 5.2 support. It is
a thin relying-party implementation with exactly the extension points this migration needs, and it
ships a DRF authentication class for the remaining API endpoints.

Three behaviours of the library were confirmed from its source and shape the design:

- The backend does not check `is_active` or call `user_can_authenticate()`, so keeping the flag as a
  gate means enforcing it by hand. It is enforced by hand, as described under identity resolution.
- The default `create_user` calls `UserModel.objects.create_user(username, email=email)`, which
  raises `TypeError` against our manager signature `create_user(email, password=None, **extra)`.
  Overriding `create_user` is mandatory, not optional.
- The library never persists the `sub` claim, so we store it ourselves.

django-allauth was rejected because it brings its own account, signup and social-account models for
no benefit here. django-pyoidc is unaudited by its own authors. Building on authlib would mean
reimplementing state, nonce and PKCE handling.

### Code layout

The OIDC code lives in `auth_prodeko`, which already owns `AUTH_USER_MODEL` and is where the new
field's migration has to go anyway.

```
auth_prodeko/
  oidc/claims.py     pure claims dict -> KeycloakIdentity dataclass, no DB and no network
  oidc/backend.py    OIDCAuthenticationBackend subclass: resolve, adopt or create, sync roles
  oidc/logout.py     end_session_endpoint URL builder
  oidc/views.py      shim views that keep the old URL names alive
  migrations/        keycloak_sub + keycloak_linked_at, and the unusable-password data migration
```

Keeping claim mapping as a pure function means the whole mapping surface is unit-testable from dict
literals, with no HTTP and no database.

### Identity resolution

The claims consumed are `sub`, `email`, `email_verified`, `given_name`, `family_name`,
`realm_access.roles` and `locale`. The registry guarantees `email`, `given_name` and `family_name`
are present for any account it manages, because it refuses login without them.

Resolution runs in this order on every callback:

1. Match `keycloak_sub == sub`. This is the steady-state path.
2. Otherwise match `email__iexact`. On a single hit, adopt the row: persist `keycloak_sub` and stamp
   `keycloak_linked_at`. Roughly 2000 of the 4000 Django rows have a Keycloak counterpart, imported
   on email; this is how each of them is reconnected to its owner, one login at a time. The
   remaining rows stay as data and become reachable again only if their owner registers in Keycloak
   with the same address.
3. Otherwise create a new user.

`verify_claims` refuses the login when `email` is missing or when `email_verified` is not true.

Membership moves to Keycloak; enablement does not. Keycloak does not issue a token for a disabled
user, so a successful callback proves the *Keycloak* account is enabled, which says nothing about
whether prodeko.org wants it. `is_active=False` is how this site takes someone out of the matrikkeli
directory and its CSV export, and no login may undo that, so a deactivated row is refused before it
is resolved: it is not adopted by address, not resolved by subject, and does not grandfather anyone.
`is_active` is never written by a login.

The population this applies to is mixed. Roughly 2000 rows carry `is_active=False`, some
deliberately deactivated and some never approved under the old registration flow, and nothing in the
data tells the two apart. Both stay refused until an administrator ticks the flag in the Django
admin, which is the only place that writes it.

Adoption never inherits privileges. `is_staff` and `is_superuser` are overwritten from Keycloak
roles on every login, so adopting a dormant staff row cannot escalate anyone.

One conflict case needs an explicit decision rather than a silent merge: a user matched by `sub`
whose Keycloak email has changed to an address another Django row already holds. The login is
refused with a clear message and the event is logged for an administrator to resolve by hand.

Matrikkeli's `Email` rows were considered as a secondary matching key and rejected. That table has
no uniqueness constraint, no case normalisation and no verification, and several Person rows can
carry the same address. It is profile decoration, not identity.

### Authorization

A `KEYCLOAK_ROLE_MAP` setting is applied on every login, granting and revoking:

- `is_superuser` from the realm role `prodeko-org-superuser`
- `is_staff` from `prodeko-org-admin`, or from being a superuser

The naming follows the lowercase-kebab convention used in the realm and the `<app>-admin` pattern
documented in the registry's `docs/keycloak-clients.md`.

`is_staff` remains the single administrator flag, gating both the Django CMS admin and matrikkeli's
admin screens as it does today. Splitting matrikkeli administration into its own role would be a
genuine least-privilege improvement, since matrikkeli holds alumni personal data and its admin can
change any member's login email, but it would mean rewriting 58 `@staff_member_required` decorators
in `alumnirekisteri/rekisteri/views.py`. That is the largest single change in the migration and it
is deliberately deferred; what matters here is that the flag now comes from Keycloak.

The make-admin, make-user, make-inactive and make-hidden actions still have to go from `views.admin`
and `views.admin_member_requests`. Every one of them writes a flag that the next login overwrites,
so leaving them would let an administrator make a change that silently reverts, which is worse than
removing the control. They are replaced by a link to the member's page in Keycloak, which is where
enablement and roles now live. This is a small, contained edit to two views and their template.

Matrikkeli's pending-approval queue goes with them, because the registration flow that filled it is
gone. The `is_active` filters on matrikkeli's search and CSV export stay as they are, and so does
the flag's meaning: every member approved under the old flow already has `is_active=True`, so the
visible population does not change, and rows that were hidden stay hidden. Setting the flag moves
to the Django admin, which is the one place left that writes it.

The make-hidden action is a no-op today in any case. It assigns `user.is_hidden`, a field that does
not exist on `auth_prodeko.User`; the assignment is discarded on save. The real column is
`Person.is_hidden`, which nothing in the UI writes.

### Who may log in

The registry creates a member row for anyone who authenticates against Keycloak. Without a gate,
just-in-time provisioning would let anyone who can register obtain a prodeko.org account and a
matrikkeli row, where today an account requires board approval.

Login is therefore gated in `verify_claims` on a realm role, `membership` or `alumni`. Both names
live in `KEYCLOAK_MEMBERSHIP_ROLES` rather than being hardcoded, so the gate widens without a code
change. Identities holding neither are shown an explanatory page instead of being signed in.

That gate on its own would be a narrowing rather than a continuation. Nothing on prodeko.org consults
membership status: `member_until`, `member_type` and `is_alumni` are matrikkeli search and export
fields, read by no view and no template, so the site has a single tier and any former member holding
an account signs in and sees everything. A role gate alone would cut all of them off.

Accounts are therefore grandfathered. `predates_keycloak` is set on every row by
`0006_grandfather_existing_accounts` and defaults to false for every account created afterwards, and
a matching row carrying it admits an identity holding no role at all. The check runs over the same
row lookup `filter_users_by_claims` resolves with, so an address already claimed by another Keycloak
subject and the break-glass address vouch for nobody, and a stranger who self-registers still matches
no row and is still refused. Just-in-time provisioning stays as closed as it was; only the accounts
that already exist are opened.

The cutoff is deliberate and it leaves a gap. Someone who joins after the migration and lets their
membership lapse is refused, where someone who joined before it is not. Closing that belongs to the
membership registry, which has to grant `alumni` automatically when a membership expires
(prodeko/membership-registry#146). The role name is in the setting already, so that work lands
without a deploy here.

### Retiring local passwords

The URL names are kept and the views behind them are replaced. `auth_prodeko:login` becomes a
redirect to `oidc_authentication_init` preserving `next`, and `auth_prodeko:logout` becomes
`oidc_logout`. This turns a sprawling template and decorator migration into two view swaps, because
every navbar link, policy modal, `login_url="/login/"` decorator and `LOGIN_URL` reference keeps
working unchanged.

Removed outright: both password-reset chains, both password-change views, the profile page's
password fields, `alumnirekisteri/auth2/forms.py`, and the login and password-reset templates in
both apps. The profile page links to the Keycloak account console instead.

A data migration calls `set_unusable_password()` on every user except the break-glass account below.
It is deliberately irreversible.

The DRF configuration drops `OAuth2Authentication`, which goes with the provider, and
`BasicAuthentication`, which becomes meaningless once passwords are unusable. `SessionAuthentication`
is enough and no OIDC bearer authentication is added: the two remaining DRF endpoints,
`app_infoscreen.SlidesList` and `tiedotteet.ContentList`, declare no `permission_classes` and are
public reads, so nothing authenticates against them today.

### Break-glass access

Keycloak runs as an Azure App Service outside this project's control, so an outage there would
otherwise lock everyone out of the CMS and the admin. `django.contrib.auth.backends.ModelBackend`
stays in `AUTHENTICATION_BACKENDS` behind the OIDC backend, and one dedicated superuser keeps a
usable password stored in the `prodeko-vault` Key Vault.

A password backend is useless without somewhere to submit a password, so this decision determines
what happens to the Django admin login page. Rather than redirecting `/admin/login/` away, it keeps
rendering the standard form with a Keycloak button added as the primary action. Every other account
has an unusable password, so the form is inert for them, and the ordinary path is one click.

The break-glass account is exempt from the unusable-password migration and from role
synchronisation, since it has no Keycloak identity to synchronise from. It is identified by a
`KEYCLOAK_BREAK_GLASS_EMAIL` setting so nothing about it is implicit, and a production deploy with
that setting empty is refused by a system check: with nothing to compare against, the exemption
matches no row and an identity registered at the break-glass address takes over the one account
that still holds a usable password. The exemption covers resolution by `sub` as well as by address,
so a subject recorded on that row by any route does not carry a login past it.

### Retiring the OAuth2 provider

`prodekoorg/app_oauth/` is deleted along with its URL include, and `oauth2_provider` is removed from
`INSTALLED_APPS`, `MIDDLEWARE`, `AUTHENTICATION_BACKENDS`, `REST_FRAMEWORK` and the
`OAUTH2_PROVIDER` setting, followed by the `django-oauth-toolkit` dependency.

The five tables are dropped by hand in a migration rather than left inert. Leaving them is not
actually free: they hold foreign keys to the user table, and with the app uninstalled Django can no
longer cascade them, so deleting any user who once held a token fails on the surviving constraint.
Running `migrate oauth2_provider zero` instead would mean keeping the app installed for one more
deploy, so the migration issues the `DROP TABLE`s itself and removes the app's `django_migrations`
rows with them. It is irreversible: reinstalling the app and migrating it forward is what brings
the tables back. They hold client secrets and issued tokens, which is a further reason not to keep
them.

Retiring the provider also touches ilmo. Its production environment still carries
`PRODEKO_OAUTH_KEY`, `PRODEKO_OAUTH_SECRET` and `PRODEKO_OAUTH_ROOT_URL`
(`ansible/roles/ilmo/templates/env.production.j2:55-72`) alongside its Keycloak configuration.
ilmo has stopped using them, so they can go: the three variables, ilmo's assert block entries and
two `kv_secret_map` entries.

### Retiring the membership application flow

`app_membership` cannot survive this migration unchanged: approving an application calls
`set_password` and emails the result in plaintext. Rather than rebuild it around Keycloak, it is
retired. The membership registry already owns applications, payment and the approval state machine,
so keeping a second implementation on prodeko.org would mean two systems deciding who is a member.

Removed: the application form and its CMS apphook, the Stripe payment intent and webhook, the
`PendingUser` model and admin screens, the acceptance and rejection emails, and the
`create_user_profile` signal that minted a Django user for every application. The two published CMS
pages become a link to membership.prodeko.org.

This drops the mailing-list automation with it. Approval currently adds the new member to
`jasenet@prodeko.org`, and to `jasenet@raittiusseura.org` for Finnish-language PoRa members, through
a Google service account with domain-wide delegation. That becomes a manual board task. The
symmetric removal never worked anyway: the `pre_delete` handler meant to take departing members off
those lists is never connected, because `app_membership/apps.py:9` has an empty `ready()`.

Payment and membership renewal move to the registry wholesale, which includes matrikkeli's own Stripe
renewal webhook at `alumnirekisteri/rekisteri/views_api.py:147`. That code is left in place rather
than deleted; once Stripe no longer points at it, it is unreachable, and removing it would mean
touching matrikkeli for no functional gain. The four `STRIPE_*` settings and their Key Vault entries
likewise stay and become dead configuration.

The Google service account file stays for a real reason: `app_poytakirjat`'s Drive integration and
matrikkeli's scanner both read it.

### Django admin

`/admin/logout/` is registered before `admin.site.urls` and redirects to `oidc_logout`.
`/admin/login/` keeps Django's own view, with a `templates/admin/login.html` override that puts a
Keycloak button above the password form; djangocms-admin-style themes that page, so the override has
to sit in the project's template directory to win. This is what makes break-glass access usable.

The `has_usable_password` branch in `prodekoorg/templates/admin/inc/branding.html:21` is replaced by
a link to the Keycloak account console.

### Sessions and logout

`OIDC_STORE_ID_TOKEN` is enabled so RP-initiated logout can send `id_token_hint`, and
`OIDC_OP_LOGOUT_URL_METHOD` builds the `end_session_endpoint` URL.

The `SessionRefresh` middleware is added. Without it, Django's 30-day `SESSION_COOKIE_AGE` outlives
any plausible Keycloak SSO session by weeks, meaning a disable or role revocation in Keycloak would
take up to 30 days to take effect. With `OIDC_RENEW_ID_TOKEN_EXPIRY_SECONDS` at 15 minutes,
revocation propagates within that window regardless of how the realm's session timeouts are set.

### The user-creation signal

`auth_prodeko/signals.py` sits directly on the just-in-time provisioning path, so it cannot be left
alone. Most of it is deleted rather than fixed, because it exists to serve `PendingUser`.

Deleted with `app_membership`: `create_user_profile`, which minted a Django user per application,
and the whole `elif hasattr(instance, "pendinguser")` branch. That branch is the worst bug in the
file. It fires on every subsequent save of a user, including the `last_login` write Django performs
at each login, and for anyone holding a `PendingUser` row it resets `member_until`, `member_type`,
`class_of_year`, `xq_year`, `city`, `ayy_member` and four privacy flags to registration defaults,
discarding admin edits and renewals. With `PendingUser` gone there are no such rows and the branch
has nothing to act on.

What remains is the `post_save` handler that creates a matrikkeli `Person` for a new user, and it
needs two small corrections. Its guard reads `if not instance.is_staff or not instance.is_superuser`,
which is true unless both flags are set, so a plain superuser gets no `Person` and every matrikkeli
view reading `user.person` raises `Person.DoesNotExist` for that account. And it assigns to the
reverse accessor and calls `save()` inside `post_save`, causing a redundant update and a re-entrant
dispatch.

## Keycloak configuration

Registered by hand in the production admin console, following the registry's
`docs/keycloak-clients.md`.

- A confidential client `prodekoorg`, standard flow only, PKCE `S256`, no implicit, device or CIBA
  flow, and direct access grants off.
- Root and home URL `https://prodeko.org`, redirect URI `https://prodeko.org/oidc/callback/`,
  post-logout redirect `https://prodeko.org/`, web origin `https://prodeko.org`.
- On the client's dedicated scope, the two protocol mappers the guide specifies: a `User Realm Role`
  mapper, multivalued, claim name `realm_access.roles`, added to the ID and access tokens; and a
  `User Attribute` mapper for `locale`, added to the ID token. The ID token setting on the roles
  mapper is the one Keycloak's default `roles` scope lacks, and without it the token carries no roles
  at all.
- Realm roles `prodeko-org-admin` and `prodeko-org-superuser`. The membership gate uses the existing
  `membership` role.

Verify the result before deploying, as the guide describes: `Client scopes` then `Evaluate`, enter a
member's username, and read the generated ID token. Those are exactly the claims the backend will
receive.

This design needs no registry-managed user attributes, so the `registry-attributes` scope is not
involved either way.

## Infrastructure changes

The ilmo Keycloak wiring is the precedent; its reference commit touched three files and eleven
lines. The prodeko_org equivalent differs in one respect: the role has no `env_file`, and
configuration reaches the container through the `variables.txt` INI bind mount.

- `ansible/vars/prodeko_vm.yml`: one `kv_secret_map` entry,
  `prodekoorg_keycloak_client_secret: prodekoorg-keycloak-client-secret`.
- `ansible/roles/prodeko_org/tasks/main.yml`: one assert and one matching `fail_msg` line. The
  assert is the only real guard, because `kv_secrets` uses `failed_when: false` and a missing secret
  otherwise passes silently.
- `ansible/roles/prodeko_org/templates/variables.txt.j2`: a `[KEYCLOAK]` section with `ISSUER`,
  `CLIENT_ID` and `CLIENT_SECRET`.
- The secret itself is created out of band in the `prodeko-vault` Key Vault.
- A gatus check on `https://id.prodeko.org/realms/membership-registry/.well-known/openid-configuration`,
  which is the actual dependency rather than the Keycloak root that is already monitored.
- `ansible/host_files/prodeko-vm2/Caddyfile.j2`: `www.prodeko.org`, `prodeko.fi` and
  `www.prodeko.fi` become permanent redirects to `prodeko.org` instead of being served directly, so
  a single OIDC redirect URI covers the site. `CSRF_TRUSTED_ORIGINS` and `ALLOWED_HOSTS` can be
  narrowed to match.

Two operational details constrain the rollout. Rendering `variables.txt` does not restart the
container, so Ansible has to run before the deploy that needs the new section. And `deploy.sh` runs
`collectstatic` before `migrate`, both performing a full `django.setup()`, so the new settings must
read with safe fallbacks or the first deploy fails before it reaches the migration step.

Separately, `prodekoorg/settings/base.py:18` constructs `configparser.ConfigParser()`, which uses
`BasicInterpolation`. A client secret containing `%` would raise `InterpolationSyntaxError` at
startup. This changes to `ConfigParser(interpolation=None)`.

## Testing

The existing suite authenticates with `client.force_login` and never exercises login views, so it
survives the change. Three tests assert on login redirect URLs and need updating:
`app_poytakirjat/tests/test_views.py:20` and `:33-36`, and `app_toimarit/tests/test_views.py:24-27`
and `:38-41`.

New tests:

- Claim mapping, as pure functions over dict literals.
- Backend resolution: adoption by `sub`, adoption by email, creation, the email-collision refusal,
  and rejection when `email_verified` is false.
- Role synchronisation, covering both granting and revoking, and confirming that adopting a row with
  stale `is_staff` does not preserve it.
- Refusal for an account holding no membership role.
- The break-glass account: exempt from the unusable-password migration, exempt from role
  synchronisation, and still able to authenticate through `ModelBackend`.
- View shims: `auth_prodeko:login` redirects to the OIDC init URL with `next` preserved, and
  `/admin/login/` still renders a form.

CI enforces `ruff check`, `ruff format --check` and `pytest --cov-fail-under=58`.

## Local development

Development points at the Keycloak that membership-registry's own compose stack already runs on
`localhost:8180`, using a local-only client registered as the guide's local development section
describes. No Keycloak service is added to this project's compose file.

The dev superuser in `docker-entrypoint.sh` keeps its password and keeps working, since
`ModelBackend` stays for break-glass anyway. Contributors who are not touching authentication
therefore need no Keycloak at all.

## Rollout

1. Register the `prodekoorg` client and its two protocol mappers, create the `prodeko-org-admin` and
   `prodeko-org-superuser` roles, verify the generated ID token with `Client scopes` then `Evaluate`,
   and store the client secret in Key Vault.
2. Create the break-glass superuser's password and store it in Key Vault.
4. Run Ansible: the `[KEYCLOAK]` section, the Caddy canonicalisation and the gatus check.
5. Deploy the relying-party code with the shim views, role sync, the unusable-password migration and
   the `app_oauth` removal.
6. Verify in production: member login, admin login through Keycloak, break-glass login through the
   admin form, matrikkeli access, and that a role change in Keycloak takes effect within the session
   refresh window.
7. Retire the membership application flow and repoint the CMS pages at membership.prodeko.org.
7. Remove the `PRODEKO_OAUTH_*` configuration from the ilmo role and its two Key Vault entries.

## Out of scope

The guiding constraint is to change as little of prodeko_org and matrikkeli as possible. Code,
views and secrets that become dead are left dead rather than chased down, so long as nothing
depends on them.

- Splitting matrikkeli administration out of `is_staff`. Worth doing, but it means rewriting 58
  decorators, and the migration is not the place for it.
- Matrikkeli's Stripe renewal webhook and the `STRIPE_*` settings, left in place as dead code once
  the registry owns payment.
- Automatic matrikkeli Person creation when a member is created in the registry. The registry has no
  webhook, event bus or readable API, so the only options are polling Keycloak with a service account
  or adding an outbound event to the registry. Just-in-time creation on first login covers the case
  adequately for now.
- The matrikkeli scanner API at `alumnirekisteri/rekisteri/views_api.py:100-144`. It has no
  authentication, accepts a raw Django session key in the request body, and returns the matching
  user's email, name and membership type. Anyone holding a session key can enumerate identities.
  This is a live vulnerability, unrelated to SSO, and should be handled on its own timeline.
- Dead code found during the audit: `alumnirekisteri/rekisteri/routers.py`,
  `alumnirekisteri/auth2/models.py` and its migrations, the `django-audit-log` dependency and the
  `admin_log` view that raises `AttributeError` on every request, and the broken
  `views.register` which violates the `Person.user` uniqueness constraint.

## Decisions

Settled during design, recorded here because each shaped the sections above.

- The membership application flow on prodeko.org is retired rather than rebuilt, and its Google
  Groups automation becomes a manual board task.
- Production keeps one break-glass superuser with a local password, which is why Django's admin
  login form stays reachable.
- Login is gated on the `membership` realm role, not on any Keycloak account.
- Matrikkeli administration stays merged with `is_staff`; only its source moves to Keycloak.
- Stripe and membership renewal move to the registry entirely; prodeko_org keeps the dead code.
- The site canonicalises to `prodeko.org`, so there is one redirect URI.
- `has_accepted_policies` stays a Django field with its site-wide modal. The consent is specific to
  prodeko.org, the text is editable from the CMS, and moving it to Keycloak would re-prompt everyone.
- The `oauth2_provider` tables are left in place rather than dropped.
- Matrikkeli's pending-approval queue is removed.

## Still to confirm

Whether ilmo has genuinely stopped using prodeko.org as an OAuth provider. Its production
environment still carries the `PRODEKO_OAUTH_*` variables alongside its Keycloak configuration, and
this gates the last rollout step only, not the migration itself.
