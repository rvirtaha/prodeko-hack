# Security findings: alumnirekisteri

Three authorization and session-handling defects in the matrikkeli app, ordered by what to fix
first. Each one is verified against the code rather than inferred, and every claim below carries a
file and line so it can be checked without repeating the analysis.

The app is mounted at `prodekoorg/urls.py:70` inside `i18n_patterns`:

```python
path(_("matrikkeli/"), include("alumnirekisteri.rekisteri.urls")),
```

so the paths quoted here are live as `/fi/matrikkeli/...`. Caddy reverse-proxies the whole site to
gunicorn with no path-level restrictions, and no middleware narrows any of this.

## Finding 1: the membership QR code is a live session cookie

The QR code on the membership card screen encodes the raw Django session key, which is the value of
the `sessionid` cookie. At `alumnirekisteri/rekisteri/views.py:913-914`:

```python
    # Data to be encoded
    data = request.session.session_key
```

That value goes straight into the image at `views.py:923` (`qr.add_data(data)`) and is rendered as
base64 PNG into `myprofile/myprofile_membership.html`. Sessions are database-backed
(`prodekoorg/settings/base.py:439`, `SESSION_ENGINE = "django.contrib.sessions.backends.cached_db"`)
and long-lived (`prodekoorg/settings/base.py:438`):

```python
SESSION_COOKIE_AGE = 60 * 60 * 24 * 30  # 30 days
```

Anyone who photographs a member's phone while the membership card is on screen, or who is handed a
screenshot of it, holds that member's session cookie. Pasting it into a browser as `sessionid` is a
complete account takeover, valid for up to a month and surviving the member's own re-login. The
attacker gets the victim's full matrikkeli profile, their settings, their ability to edit or delete
their own data, and — by way of Finding 2 — everyone else's data too. No credential is needed and
nothing about the act looks unusual: membership cards are shown to door staff and photographed at
events as a matter of routine.

The existing SSO overview understates this. At
`docs/superpowers/specs/2026-08-14-keycloak-sso-overview.md:142-145` it reads:

> One thing found along the way should not wait for any of this. Matrikkeli's membership card scanner
> has an interface that is not protected at all, and anyone who obtains the code from a member's phone
> screen can read that member's name, email address and membership type.

Read on its own, that describes a disclosure of three low-sensitivity fields via one endpoint. The
actual exposure is account takeover, and the scanner endpoint is not required to achieve it — the
code in the QR is the credential itself. Anyone triaging from that paragraph will size the work
wrongly.

### Remediation

Stop putting the session key in the QR. Encode a signed, short-lived, single-purpose token instead:
mint it with `django.core.signing.TimestampSigner` (or a dedicated `Signer` with a distinct salt such
as `matrikkeli.membership-card`) over the person's primary key, give it a lifetime measured in
minutes, and have `membership_status` regenerate it on each page load. The scanner endpoint then
calls `unsign(token, max_age=...)` and resolves the person from the payload, so a photographed code
expires on its own and confers nothing beyond being scanned once. Rotate existing sessions when this
ships, since any key already photographed stays valid until its 30 days run out.

## Finding 2: the sub-entity views perform no ownership check

Thirty-six views at `alumnirekisteri/rekisteri/views.py:1090-1784` — add, edit and delete for Phone,
Email, Skill, Language, Education, WorkExperience, PositionOfTrust, StudentOrganizationalActivity,
Volunteer, Honor, Interest and FamilyMember — take the target's primary key from the URL and never
compare it to the caller. They share one shape exactly; Phone is representative.

```python
# views.py:1090-1091, 1098
@login_required(login_url="/login/")
def add_phone(request, person_pk):
            obj.person = Person.objects.get(pk=person_pk)

# views.py:1107-1108
def edit_phone(request, pk):
    obj = get_object_or_404(Phone, pk=pk)

# views.py:1130-1131
def delete_phone(request, pk):
    obj = get_object_or_404(Phone, pk=pk)
```

The evidence that nothing else supplies the missing check:

- `login_required` at `views.py:16` is the stock `django.contrib.auth.decorators` import, not a
  local wrapper. Grepping `alumnirekisteri/` and `auth_prodeko/` for `def .*_required` and
  `user_passes_test` returns nothing.
- Grepping the whole range `views.py:1090-1785` for `request.user` returns zero hits. The only
  matches for `person` are the twelve identical `obj.person = Person.objects.get(pk=person_pk)`
  lines in the `add_*` views.
- The forms cannot be filtering, because they take no user. `PhoneForm`
  (`alumnirekisteri/rekisteri/forms.py:718-726`) is a bare `ModelForm` whose entire contract is
  `fields = ["phone_number", "number_type"]` at `forms.py:721` — no `__init__` override, no queryset
  scoping. The other eleven match.
- No global mixin or middleware applies. These are function views, and `MIDDLEWARE`
  (`prodekoorg/settings/base.py:189-209`) is stock Django plus CMS, CORS and OIDC `SessionRefresh`.
- `alumnirekisteri/rekisteri/urls.py:36-198` routes each view directly under `myprofile/`. That path
  segment is cosmetic; nothing binds it to the caller.

The codebase already knows the right pattern, which makes this an omission rather than a design
choice. `DeleteProfileForm` at `forms.py:983-991` takes the user and validates against it:

```python
    def __init__(self, user, *args, **kwargs):
        if username.strip().lower() != (self.user.email or "").lower():
```

The write side is the obvious half: any authenticated member can delete another member's phone
numbers, emails, employment history and family entries, or plant fabricated entries on a named
alumnus's profile with a single `POST` to
`/fi/matrikkeli/myprofile/add-work-experience/<victim_person_pk>`.

The read side is the half most likely to be underestimated. Both `edit_*` and `delete_*` disclose
the record on `GET`, before any form is submitted and therefore with no CSRF token involved at all.
`edit_phone` renders the modal with the form bound to the instance (`views.py:1109`,
`form = PhoneForm(instance=obj)`), so every field comes back pre-populated. `delete_phone`
interpolates the value directly into the confirmation text at `views.py:1140`:

```python
            "text": "Poistetaanko {}?".format(obj.phone_number),
```

and `delete_family_member` does the same with `obj.get_full_name()` at `views.py:1781`. A logged-in
member walking `pk=1,2,3...` with plain `GET` requests therefore dumps the phone numbers, private
email addresses, family members and employment history of the entire register. This bypasses the
privacy flags on `Person` — `show_name_category` (`models.py:36`), `show_address_category`
(`models.py:45`) and `show_personal_category` (`models.py:52`) — because those flags are consulted
only by the profile rendering path (`models.py:271-287`) and never by these views. A member who has
marked their details private is as exposed as one who has not.

Under GDPR this is an unauthorised-access and integrity failure over personal data held by a
registered association, including marital status, family members and military rank.

### Remediation

Because all thirty-six views share one shape, a shared decorator is the smaller change than editing
each body. Write two decorators in `views.py`:

- For the `edit_*` and `delete_*` views, one that takes the model class, fetches
  `get_object_or_404(Model, pk=pk, person=request.user.person)` and passes the object into the view.
  Scoping the fetch rather than fetching-then-comparing means a mismatch returns 404 instead of 403
  and leaks no existence information.
- For the `add_*` views, one that rejects any `person_pk` other than `request.user.person.pk`, so
  `Person.objects.get(pk=person_pk)` can only ever resolve to the caller. This also removes the
  uncaught `Person.DoesNotExist` that a bogus `person_pk` currently turns into a 500 and an email to
  the CTO.

Staff need a way through for the admin edit screens, so let both decorators pass when
`request.user.is_staff`, matching the gate already used at `views.py:226` and `views.py:871`.

## Finding 3: the scanner endpoint is an unauthenticated confused deputy

Recorded for completeness. The door-scanner feature is being retired, so this needs no work beyond
confirming the endpoint goes with it.

`class Scanner(View)` at `alumnirekisteri/rekisteri/views_api.py:100`, routed at
`alumnirekisteri/rekisteri/urls.py:220`, carries no authentication decorator and no permission
class. The staff gate sits only on the page that hosts the scanner UI (`views.py:226`,
`@staff_member_required` on `admin_qr_scanner`), not on the API it calls. Both the session key and
the spreadsheet ID come from the request body (`views_api.py:103-104`), and the spreadsheet ID is
passed through unchecked at `views_api.py:121` (`direction = modify_sheet(sheet_id, user)`) to a
service account with full Drive scope:

```python
# views_api.py:38, 41
    SCOPES = ["https://www.googleapis.com/auth/drive"]
        SERVICE_ACCOUNT_FILE, subject="mediakeisari@prodeko.org", scopes=SCOPES
```

Anyone holding one valid session key can therefore make the server read and append rows to any
spreadsheet that `mediakeisari@prodeko.org` can reach. The `HttpError` branch reflects Google's error
text back to the caller at `views_api.py:133` (`{"active": False, "message": f"Tapahtui virhe:
{error}"}`), which turns the endpoint into an existence and permission oracle for arbitrary
spreadsheet IDs. Without a valid session key the caller gets only the canned "not logged in"
response, and session keys are 32 characters of Django-random, so this is an amplifier for keys
obtained via Finding 1 rather than a standalone break.

Remediation is to delete the route and the view with the feature. If any part of it survives,
require staff authentication on the endpoint and pin the spreadsheet ID to a server-side setting
instead of accepting it from the request.

## Mitigating factors and the absence of a trail

CSRF protection is intact and does real work here. `CsrfViewMiddleware` is active
(`prodekoorg/settings/base.py:194`), the delete flow posts a token
(`templates/modals/delete_modal.html:2`), and neither `SESSION_COOKIE_SAMESITE` nor
`CSRF_COOKIE_SAMESITE` is overridden anywhere in `prodekoorg/settings/`, so Django's `Lax` default
applies and a cross-origin `POST` carries neither cookie. A malicious third-party page cannot drive
any of Finding 2 against a visiting member. Exploitation needs an insider with an account, or a
stolen session.

That protection does not extend as far as it first appears. It is no barrier to a member acting
deliberately, since the attacker's own token is valid against any target's primary key. It is no
barrier on the read side of Finding 2, which is all `GET`. And it is no barrier to Finding 3: an
unauthenticated caller obtains a `csrftoken` cookie with one `GET` to any page carrying a form and
then supplies the matching header, exactly as the legitimate client does at
`static/js/qr_scanner.js:43`.

There is also no way to tell from the application whether any of this has already been used. The
audit-log middleware that would have recorded it is commented out at
`prodekoorg/settings/base.py:207`:

```python
    # "audit_log.middleware.UserLoggingMiddleware",
```

and the per-model hook is commented out on every affected model, including `Phone`
(`alumnirekisteri/rekisteri/models.py:329`) and `Email` (`models.py:338`):

```python
    """ audit_log = AuditLog() """
```

Every action described above is authenticated and therefore attributable in principle, but in
practice nothing writes that attribution down. Answering "has this happened" requires web server
access logs, if they are retained long enough to cover the window.
