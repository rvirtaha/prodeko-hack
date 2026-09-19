# Matrikkeli: reference documents

Matrikkeli is Prodeko's alumni register. It lives today inside the Django CMS
that is being retired, and it becomes its own service. These are the documents
that service is built from.

Read them in this order.

- [matrikkeli-migration.md](matrikkeli-migration.md) — the plan. What the new
  service is, what it deliberately does not do, the stack, what happens to every
  table in the old schema, and the four phases from profiling the dump to
  cutover. Start here; everything else is supporting material.
- [security-findings-alumnirekisteri.md](security-findings-alumnirekisteri.md) —
  the authorization defects in the current app. Finding 2, thirty-six sub-entry
  views with no ownership check, is the reason a new boundary is being drawn
  rather than the old one patched. These are fixed in the old app separately;
  they are here so the new one cannot reproduce them.
- [2026-08-14-keycloak-sso-design.md](2026-08-14-keycloak-sso-design.md) — how
  accounts work at Prodeko. It defines the `keycloak_sub` join that connects a
  person to an identity, and it is the source of every volume figure quoted in
  the plan. [The overview](2026-08-14-keycloak-sso-overview.md) is the short
  version.
- [roadmap.md](roadmap.md) — the wider project this belongs to, and the
  deployment convention the plan refers to: a Dockerfile in the application
  repository, Ansible in infra-prodeko.

## What is not here

The source code. `prodeko-org-djangocms` holds the app being replaced and
remains available to read; the plan cites it by file and line throughout. Four
places in it are worth opening early, because the plan depends on them and they
are easy to overlook:

- `alumnirekisteri/rekisteri/models.py` — the fourteen models the ETL is written
  against.
- `Person.get_tuta_educations()` in that file — twenty-eight `icontains` clauses
  matching sixty years of spellings of the university's name. Port it verbatim
  and treat it as data, not logic; the CSV export's degree-year columns stand on
  it.
- `views.admin_export_data` — roughly three hundred lines that are the
  specification of the staff CSV column picker.
- `rekisteri/tests/test_delete_profile.py` — the behaviour of self-service
  deletion, which the new service keeps.

Production data is not here either, and was never read. Every number in the plan
about how much register there is comes from prose in the Keycloak document, not
from a database. Phase 0 exists to replace those estimates with counts.
