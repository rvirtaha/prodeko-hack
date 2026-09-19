# Matrikkeli migration plan

The alumni register is the one part of prodeko.org that cannot become a Hugo
page. It is a database of living people, it is edited by the people it
describes, and the hackathon brief keeps it separate from both the website and
the member registry. This is the plan for moving it.

It is written against the Django source in prodeko-org-djangocms and the Rust
membership registry in membership-registry. Where it states a number that came
from reading code, the file is cited. Where it states a number about production
data, it says so, because no production data was available to read.

## What matrikkeli is today

One Django app, `alumnirekisteri/`, about 3700 lines, running inside the same
process as the CMS. `matrikkeli.prodeko.org` is a Caddy redirect to
`/fi/matrikkeli/`, not a separate deployment. Fourteen models in
`alumnirekisteri/rekisteri/models.py`: a `Person` with roughly a hundred
columns, twelve child tables hanging off it by cascade, and a mailing-list
table.

What actually works is a directory. A member signs in, searches by name or
starting year, and reads someone's profile. They edit their own profile across
twelve kinds of sub-entry — phone numbers, email addresses, skills, languages,
educations, work experiences, positions of trust, student organisational
activities, volunteering, honours, interests, family members — and upload a
photograph. Staff pull a tab-separated CSV with a column picker
(`views.admin_export_data`) and generate the printed matrikkeli as LaTeX
(`views.admin_export_matrikkeli`).

A good deal of the rest does not work. The management commands import a user
model that no longer exists. The audit page 500s. The bulk photo export builds
a zip from a local directory that production does not use, because production
media lives in Azure Blob Storage behind `static.prodeko.org`
(`prodekoorg/settings/prod.py:50`). The REST serializers are wired to nothing.
`alumnirekisteri/auth2/` is an entire second user model kept only by inertia,
and `alumnirekisteri/rekisteri/routers.py` routes a database that is not
configured — both named as dead in
`prodeko-org-djangocms/docs/superpowers/specs/2026-08-14-keycloak-sso-design.md:442`.

The app also carries known authorization defects, the shape of which matters
here even though the detail belongs elsewhere. The thirty-six add, edit and
delete views for sub-entries check that you are logged in and nothing else:
`views.edit_phone` does `get_object_or_404(Phone, pk=pk)` and saves the form
(`alumnirekisteri/rekisteri/views.py:1106`), so any member can edit any other
member's rows by guessing an integer. Thirty-six views, one missing predicate
each. These are being written up in the prodeko-org-djangocms repository and are
not this document's subject; what they tell the migration is that the defect is
structural rather than incidental, and a new boundary is what removes the class.

### Why it has to move now

The CMS is being retired, and matrikkeli is inside it. The coupling is
shallower than that sounds — exactly one import crosses the boundary, the
`post_save` handler at `auth_prodeko/signals.py:5` that creates a `Person` for
every new account — so extraction is not the hard part. Deciding what comes out
the other side is.

## The target: a new lightweight standalone app

This is decided. Matrikkeli becomes its own small application with its own
database and its own repository, authenticating through Keycloak at
id.prodeko.org the way the editor proxy and the membership registry already do,
deployed the way [the roadmap](roadmap.md) describes for everything else: a
Dockerfile in the application repository, Ansible in infra-prodeko.

The stack is Go: server-rendered HTML templates, Postgres, SQL migrations
checked into the repository, one static binary in the container. Go because the
guild already runs Go in production in `cms-auth-proxy`, because a directory
with a search box and a set of forms needs nothing a standard library and a
database driver do not provide, and because a volunteer who has never seen the
codebase can read a Go handler and know what it does. No ORM — the schema is
small enough that the queries are the documentation.

The front end is htmx over those same templates. The register is a search box
and a set of forms, which is the shape htmx exists for: a handler renders a full
page or a fragment depending on `HX-Request`, so there is one set of templates
and no JSON API to keep in step with them. Search results update as you type and
a sub-entry row is added or removed in place, without any of it being a separate
application with its own build.

Two rules that are free at the start and expensive later. Every form works
without htmx — an ordinary POST that redirects, with htmx intercepting it — so
the register degrades to plain HTML rather than breaking. And htmx is vendored
into the repository at a pinned version, not loaded from a content delivery
network, because a register of personal data should not fetch executable code
from a third party at page load.

Four reasons, in descending order of how much they weigh.

**The authorization hole cannot survive a new boundary.** The thirty-six
unscoped views exist because each sub-entry type got its own copy-pasted
triplet of add, edit and delete. Fixing that in place means adding thirty-six
ownership predicates and remembering all thirty-six, with no test that fails
if you miss one. Every child table has the identical shape — an integer key, a
`person` foreign key, some fields — so in a new service they are one
owner-scoped handler, and there is no code path that can reach a row except
through the person who owns it. A new boundary fixes this structurally.
Patching the existing views fixes it by vigilance.

**Person-without-account has to be native.** In the current schema `Person.user`
is nullable, but nothing ever creates a person without a user: the signal makes
one per account, and migration `0004_backfill_profiles` made one for every
account that predated the signal. The result is a register that can only
describe people who hold a login, which is the opposite of what a sixty-year
alumni register is for. The historically valuable rows — the officials from the
seventies, the deceased, the people whose last known address is a paper card —
are exactly the ones with no account. A schema where the account is an optional
attachment rather than the primary key is a different schema, not a patched one.

**Consent has to become a record rather than a default.** There is no consent
model in the current system. No consent table, no timestamp, no policy version,
no lawful basis, no retention field, and not one `auto_now_add` on any of the
fourteen models. What exists is eighteen `show_*` booleans with mixed defaults,
several of them defaulting to true, applied to two thousand profiles that a
backfill migration created for people who have never opened the settings page.

How far that drifted is visible in what the register has been publishing.
`templates/matrikkeli.tex`, which rendered the printed volume, prints home
address, birth date, gender, marital status, military rank, phone numbers and
family members — and reads **none** of the eighteen flags. Its only gate is
`dont_publish_in_book`, which defaults to false, meaning publish, and which
`management/commands/flip_publish_in_book.py` once bulk-inverted for everyone
who had ever logged in. So the same person's data has been governed by two
unrelated consent models at once, both opt-out by default, one of them over a
physical disclosure that cannot be corrected after printing. Neither model can
say when anybody agreed to anything, because neither records a time.

That is the argument for one explicit consent model in the target, with a
timestamp on every answer. Adding a consent table to the old schema is possible;
making the old columns mean something they have never meant is not.

**A small purpose-built application matches how the rest of this is being
built.** The new site is static files and a few hundred lines of proxy. The
membership registry is a Rust service with SQL migrations. A matrikkeli in the
same spirit — one database, one service, readable end to end in an afternoon —
is a thing the guild can keep alive with volunteers. Another Django project,
maintained on a supported Django version for the next decade as the guild's
only remaining Django project, is not.

The service that comes out is smaller than the one that goes in. The membership
block leaves entirely (below), the dead code does not come, thirty-six views
collapse into one, and two features are retired rather than carried: the printed
book and the door scanner. What remains is search, a profile page, owner-scoped
editing, photo upload and the staff CSV.

### The alternative considered and closed: lift the Django app out

Take `alumnirekisteri/` into its own Django project, point it at the same
database, delete the CMS. It is the cheaper path — the extraction is shallow,
there is no ETL because it is the same database, and the Django admin and the
CSV export keep working untouched — and it was the initial recommendation.

It is not taken, because everything that comes free includes every defect: the
hundred-column table, the eighteen flags with no timestamps, the person that
requires an account, the thirty-six unscoped views, and a membership block
that has to be deleted regardless. Performing that surgery inside the existing
codebase is not meaningfully less work than writing a small clean application,
and at the end of it the guild still owns a Django project — its only one, once
the CMS is gone. The new app's cost is concentrated, visible and ends. The
lift's is spread across years of maintaining a schema nobody would design.

Recorded here so the question is not reopened without new information. The one
thing that would reopen it is a constraint on people or time: if the migration
has to happen in weeks, or with a single volunteer, the lift preserves the data
and buys time, and is the better answer under those conditions.

### What the new app must not lose

Four things, in rough order of how easy they are to lose by accident.

**The staff CSV export.** Thirty-odd optional columns with a picker
(`views.admin_export_data`). This is how alumni relations work actually gets
done, and it is the feature most likely to be dismissed as legacy and then
urgently missed.

**The exclusion semantics.** `is_active=False` and `Person.is_hidden` take
someone out of search and out of the export. People who were removed stay
removed. The new schema needs an equivalent that no login can undo, and it
should be one flag rather than two on different tables.

**Self-service deletion.** `views.delete_profile` works today, deletes all
twelve child collections and the account, and is covered by five tests in
`rekisteri/tests/test_delete_profile.py`. Keep it, and fix the part that is
missing: it never deletes the profile photograph from the blob container.

**`Person.get_tuta_educations()`.** Twenty-eight `icontains` clauses matching sixty
years of free-text spellings of TKK, STKK, HUT, Teknillinen korkeakoulu and
Aalto, and of every name the degree programme has had. It looks like bad code
and it is accumulated institutional knowledge. It is what `get_starting_year()`
and `get_graduation_year()` stand on, and those are what the CSV export's
degree-year columns stand on, so it is load-bearing for the one export that
survives. Port it verbatim and treat it as data, not logic.

## Membership truth leaves

`member_until`, `member_type`, `class_of_year` as a membership fact,
`ayy_member`, `pora_member`, `is_alumni` — the whole block goes to
membership-registry, which already owns it, and the Stripe renewal webhook in
`views_api.StripeWebhook` goes with it. The door scanner is retired outright and
does not reappear in the target. Matrikkeli reads membership status where it
needs it, from the token's realm roles or from the registry, and stores none of
it.

This is not tidying. Keeping a second copy of membership state in a second
database guarantees the two diverge, and the register has no way to be right
when they do. It also removes the largest chunk of what makes the current app
complicated. What is left is a profile directory: search, profile, self-edit,
and one staff export.

The same principle settles the name and email question. Keycloak already
overwrites first name, last name and email on every login, so matrikkeli's
settings form edits those fields and the next sign-in silently reverts them.
The new service does not offer to edit what it does not own.

## The data reality

### Shape

Fourteen models. `Person` carries name variants and a unique `slug`; a home
address block; birth date, place of birth, gender and marital status; military
rank and promotion year; the membership block that is leaving; student number;
an `admin_note` free-text column; homepage and LinkedIn; a photograph;
`is_hidden` and the eighteen `show_*` flags; and twenty-seven boolean tags
across twelve industries and fifteen job functions plus a free-text
`function_free`.

The twelve child tables are uniform: integer key, cascading `person` foreign
key, a handful of fields, no timestamps. `WorkExperience` is the odd one — it
carries a second full address block with its own `show_address_category`.
`Email` has no uniqueness constraint, no case normalisation and no
verification. `FamilyMember` holds names, birth surnames and professions of
spouses, children, parents and siblings.

### Volume: unprofiled

This is the most important honest statement in the plan. **No production data
was available to read.** The `data.json` at the root of prodeko-org-djangocms
looks like a database dump and is not one for these purposes: 2852 objects, all
of them CMS pages, placeholders, plugins and grid rows, with zero `rekisteri`
rows and zero `auth_prodeko` rows. The only other fixture in the repository
holds two test accounts. There are no SQL dumps and no CSVs of people.
`alumnirekisteri/old_data_migrator.py` reads fourteen CSVs from an
`old_data_files/` directory that is not in the repository and not on any
machine reachable from here; the filenames are the only surviving description
of the pre-Django schema.

Every volume figure in circulation traces to prose in
`docs/superpowers/specs/2026-08-14-keycloak-sso-design.md:101-118`, written
from a live audit during the Keycloak work: roughly four thousand account rows,
roughly two thousand with a Keycloak counterpart, roughly two thousand carrying
`is_active=False` — a mix of deliberate deactivations and registrations that
were never approved, with nothing in the data distinguishing the two. Since
migration `0004` guarantees one `Person` per account, expect about four
thousand person rows of which perhaps half are reachable people.

Nobody has counted how many of those four thousand are filled in. That number —
persons with at least one education, work experience or contact row — is the
only one that predicts how much work the ETL is and how much register there
actually is. It is the first thing phase 0 measures.

### Identity

The join is `Person.user_id` to `auth_prodeko.User`, then `keycloak_sub` to the
registry. `auth_prodeko/models.py` carries `keycloak_sub` as a unique nullable
column with a `keycloak_linked_at` timestamp, and
`membership-registry/main/backend/migrations/20240526132953_initial.up.sql:3`
defines `Member.user_id uuid PRIMARY KEY` with the comment "id comes from
identity service". The Keycloak subject is literally the registry's primary
key, so where `keycloak_sub` is populated the join is exact.

It is populated lazily, one login at a time: the OIDC backend resolves by
subject, falls back to a case-insensitive email match, and stamps the subject on
success. So coverage is a function of who has signed in since the Keycloak
cutover, not of who exists. For everyone else the only key is
`auth_prodeko.User.email`, which is at least unique and normalised.

Matrikkeli's own `Email` rows are not a fallback and should not be treated as
one — no uniqueness, no normalisation, no verification, several persons can
carry the same address. The Keycloak design document rejects them explicitly as
"profile decoration, not identity".

**The coverage cliff is the central data fact of this migration.** Roughly half
the rows join to nothing, and it is not a random half: it is the dead, the
unreachable, and the historical officials — precisely the part of the register
that a sixty-year-old guild values most. Any target schema in which a person
requires an identity loses them.

## Phases

### Phase 0 — profile the real dump

Before any schema is agreed. Obtain a read-only dump of the production
`rekisteri_*` and `auth_prodeko_user` tables and measure, in a scratch
database, never on a laptop:

Row counts per table. Null rate per column on `Person` — this is what decides
which of the hundred columns exist in the target at all. The filled-profile
count, by the definition above. The distribution of child rows per person, so
the long tail is visible rather than averaged away. How many persons have a
populated `keycloak_sub`, how many join by email only, how many join to
nothing. How many carry `is_active=False`, `is_hidden`, `is_dead`. How many have
a photograph path, and how many of those paths resolve to a blob that exists.
How many `admin_note` values are non-empty and how long they run. How many
slugs are numeric — the signal writes the primary key as the slug — versus
name-derived, which tells you how many member-facing profile URLs are
human-readable and would break.

Two things this phase settles that nothing else can. Whether the dump the
roadmap calls "already available" actually contains the register tables, which
is currently an assumption. And whether the four-thousand figure is right,
which everything downstream is sized against.

Output is a one-page table of counts. No personal data leaves the scratch
database.

### Phase 1 — schema and ETL

The target schema is small. A `person` table with a nullable `keycloak_sub`, so
a person without an account is an ordinary row rather than an exception. The
twelve child tables essentially as they are, since their shape is fine. The
twenty-seven industry and function booleans normalised into a tag join table
during the load, because doing it later means a second migration. An append-only
`consent` table — person, category, granted, decided at, policy version, source
— where the current state is the latest row per category and the history is the
audit trail the old system never had.

The load, per model:

| model | call | notes |
|---|---|---|
| `Person` name, slug, photo path | migrate | slug carried verbatim |
| `Person` membership block | drop | membership-registry owns it |
| `Person` address, birth date, gender, marital status, military, student number | migrate closed | consent, below |
| `Person` `admin_note` | do not migrate | see below |
| `Person` `dont_publish_in_book` | drop | the printed book is retired |
| `Person` `mentoring`, `ventures`, `partner_contact`, `subscribe_alumnimail` | migrate as-is | opt-in flags, see below |
| `Person` industry/function booleans | migrate, normalised | into the tag table |
| `Person` `is_hidden`, `is_active` | migrate | as one exclusion flag |
| `Education`, `WorkExperience`, `PositionOfTrust`, `StudentOrganizationalActivity`, `Volunteer` | migrate | this is the asset |
| `Skill`, `Language`, `Honor`, `Interest` | migrate | low risk |
| `Phone`, `Email` | migrate, deduplicated | `Email` needs case normalisation on the way in |
| `WorkExperience` address block | migrate closed | it is a home-adjacent address like any other |
| `FamilyMember` | migrate closed | third-party data, see below |
| `MailList`, `subscribed_lists` | drop | mailing lists belong in the mail tool |
| `auth2.User`, `routers.py` | drop | already dead |

**`admin_note` is not migrated.** It is free text written about a person, by
staff, without that person's knowledge, with no author and no timestamp, so
nobody can say who wrote a given note or when. It falls squarely within a
subject access request. Carrying the column forward invites it to keep growing
under exactly the conditions that made it a liability. The notes are extracted
once to a reviewed export held by a named person with a deletion date, outside
the application; anything in them with genuine operational value becomes a
structured field in the new schema — a deceased flag and a do-not-contact flag
cover most of what a note like this is usually doing — because a structured
field is the only form that survives review. The alternative, a staff-only
archive inside the service, was considered and rejected: it is the same
liability with a login in front of it, and it guarantees the field gets used
again.

**The opt-in flags migrate as they stand, and the visibility flags do not.** This
is the one place the distinction is worth stating plainly. `mentoring`,
`upper_management_mentoring`, `ventures`, `partner_contact` and
`subscribe_alumnimail` all default to false, so a true value is something a
person actively did — a decision, even though nothing recorded when. The
sensitive `show_*` flags default to true, so a true value is a default that
survived. Same data type, opposite evidential weight, and the direction of the
default is what tells them apart.

**`FamilyMember` migrates closed, and stays closed until asked.** These are
names, birth surnames, professions and relationship dates of people who are not
members, were never asked, and in most cases do not know the register exists. A
member can consent to publishing their own marital status; they cannot consent
on behalf of a spouse or a sibling. So the rows come across — losing sixty years
of genealogy to a consent technicality would be the wrong trade — but they are
invisible to every reader, including staff exports, until the member is asked
about that category specifically at re-affirmation and says yes.
`show_family_members_category` already defaults to false, so for most profiles
this is the state they were already in.

**Photographs need reconciliation, not copying.** The database stores a relative
path under `profiles/`; the bytes are in Azure Blob Storage behind
`static.prodeko.org/media/`. Two known problems. `views.delete_user` never
deletes the image, so every profile ever deleted has left its blob behind — the
container holds photographs of people who asked to be removed. And the bulk
photo export reads `MEDIA_ROOT + "/profiles"` from the local filesystem
(`views.py:212`), which production does not populate, so that feature has been
broken since the move to blob storage and nobody noticed. The migration lists
the container, joins it against surviving person rows, copies what matches, and
deletes the orphans. The orphan count is a phase 0 measurement and may be the
most useful number the profiling produces.

### Phase 2 — consent re-affirmation

The sensitive categories load closed: home address, birth date, gender, marital
status, military rank and promotion year, student number, the work address
block, and family members. Not hidden, not deleted — present in the database,
not shown to anyone, and awaiting a decision. Nothing in this phase throws data
away; it decides who may read it.

On first sign-in an alumnus sees what the register holds about them and chooses,
per category, whether it is visible to signed-in members. Each answer writes a
consent row with a timestamp, the policy version it was given under, and how it
was collected. That row is the thing the old system has never had in any form.
There is one consent model and one place it is recorded, which is the direct
answer to the two-models-and-no-timestamps state described above.

The default-on flags from the 2026 backfill are explicitly not treated as
decisions and are not carried as pre-ticked boxes. A flag set by a migration for
a person who has never opened the settings page is a default, not a choice, and
reading it as consent is the specific mistake this phase exists to avoid.

Two consequences to state up front rather than discover.

The new matrikkeli shows less on day one than the old one did, and someone will
report this as a bug. It is the point of the exercise. It should be in the
launch note.

Re-affirmation reaches only people who sign in. The roughly two thousand who
never will keep their sensitive categories closed permanently, which is the
correct outcome and also means the directory never returns to its old apparent
completeness. Career history, educations and honours are not in the closed set,
so what those profiles show is the professional record, which is most of what
the register is for.

### Phase 3 — cutover

The new service runs against migrated data while the Django app stays up and
read-only. Read-only is the whole trick: it removes the dual-write problem
entirely, and the register is not a system where a few weeks without edits costs
anything.

Verification before the old app goes away, in this order. Row counts reconcile
per table against the phase 0 baseline, with every difference explained rather
than tolerated. A sample of profiles renders identically in both, modulo the
consent closures. Search returns the same people for the same queries. The CSV
export produces the same columns for the same filters. Every photograph that
should exist resolves. Profile URLs resolve or redirect — and if a legacy slug
cannot be carried, it 404s rather than resolving to the wrong person, because a
profile URL silently pointing at someone else is worse than a dead link.

Slugs deserve one clarification. They are member-facing, not public: both
`public_profile` and `search` are `@login_required`
(`alumnirekisteri/rekisteri/views.py:1786`, `:1796`), so no anonymous visitor
and no search engine has ever resolved one. Breaking them costs bookmarks and
internal links, not search ranking. Whether they break at all is a phase 0
question — how many are numeric primary keys and how many are name-derived.

The Django database is archived, not deleted, and kept for a stated period
against a reconciliation question nobody thought of. The archive is also the
only remaining home of the `admin_note` column, so its retention date is a real
decision and not a formality.

## Risks

**The dump may not contain the register.** Everything here assumes access to
production `rekisteri_*` tables. The dump the roadmap describes as already
available is a CMS dump. Confirm early; this blocks phase 0 and phase 0 blocks
the rest.

**The coverage cliff is worse than estimated.** If far fewer than half the rows
carry a usable identity, the migration is mostly a load of orphan person records
and the consent phase reaches almost nobody. The mitigation is in the schema —
person-without-account is native — but it changes what the launched service
looks like, so it should be known before launch rather than after.

**Volume is unknown in both directions.** Four thousand rows is a manageable
number and also an unverified one. If the filled-profile count turns out to be
two hundred, the new app is smaller than planned. If child tables run to
hundreds of thousands of rows, the ETL needs more care than a script.

**Free text is sixty years dirty.** School names, employer names and position
titles are unconstrained strings across six decades, two languages and several
renamings of the same university. `get_tuta_educations` is the proof. Nothing in
this plan cleans them, and nothing should — normalising them is a separate piece
of work that needs a human who knows the history.

**Scope creeps back toward membership.** Every feature request for the new
matrikkeli will be a membership feature in disguise, because that is where the
old app's complexity lived. The boundary holds only if someone defends it.

**The deceased complicate the consent story rather than simplifying it.** Data
protection does not follow a person past death, so the `is_dead` rows are
outside the consent regime — except that their `FamilyMember` rows describe
living people, and their addresses may be a surviving spouse's. Treat a
deceased person's household data as living-person data.

**Photographs have no provenance.** No licence, no photographer, no upload date
anywhere in the schema. They migrate as-is with no way to answer a question
about any individual image.

## What the demo shows

This document is the deliverable. The working thing to aim at after the
hackathon is deliberately one slice, and it is the read path.

**The smallest first slice: a read-only directory over migrated data.** Load
person rows, educations, work experiences and the tag table into the new schema
with every sensitive category closed. Authenticate through Keycloak. Search by
name and starting year, and render a profile. No editing, no photo upload, no
exports.

It is the right first slice because it proves the two things the plan actually
rests on. The ETL runs against real data and the counts reconcile — which is
the claim the whole migration stands on and the one nobody can check by reading.
And a person with no account renders as a first-class profile, which is the
schema decision that separates this from the app it replaces and the one that is
expensive to reverse later.

Owner-scoped editing is the second slice, and it is where the thirty-six
unscoped views become one handler that cannot serve someone else's row. The
consent re-affirmation flow is the third, and photo upload and the staff CSV
are the fourth. That is the whole application: four slices, and the fourth is
the only one that is merely useful rather than load-bearing.
