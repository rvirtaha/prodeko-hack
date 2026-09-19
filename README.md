# prodeko.org

A git-based website for Prodeko. Pages are Markdown files, lists of people are
YAML files, and Hugo turns them into plain HTML. Editors sign in with their
normal Prodeko account and never see git.

See [the design document](docs/superpowers/specs/2026-09-18-prodeko-git-cms-design.md)
for how it works and [the roadmap](docs/roadmap.md) for what is being built.

## Layout

```
site/     the Hugo site, the templates, and the Decap configuration
proxy/    the service that lets editors sign in with a Prodeko account
tools/    the site audit, the front-matter fixer, and one-off importers
docs/     design, roadmap and the written deliverables
```

Deployment lives in the separate infra-prodeko repository: an Ansible role for
Caddy and the proxy container. The built site is copied to the server over SSH
and served from disk.

## Running the site locally

Hugo extended, version 0.166.0 or newer:

```
cd site
hugo server
```

That serves the whole site at http://localhost:1313/, public pages and member
pages together, with no login. Hugo runs the server in its development
environment, which mounts both content roots into one site. The server renders
to `site/public-preview/`, so a preview never leaves member HTML in the
directory that gets deployed.

The site is bilingual, with Finnish under `content/fi/` and English under
`content/en/`. Two pages are translations of each other when they share a
`translationKey` in their front matter, which is what lets the Finnish and
English addresses differ.

Search needs an index, which is built from the rendered HTML rather than from
the content, so it is a second terminal:

```
cd site
npx pagefind@1.5.0 --site public-preview
```

Hugo serves `site/public-preview/` from disk, index included, and the index
survives a live-reload rebuild. Editing a page serves the new text immediately
while the index stays as it was, so re-run that command — it takes about a
second — to see the change in search results. The preview mounts both content
roots into one site, so the preview index holds public and member pages
together.

## Public pages and member pages

Some pages are for Prodeko members only: meeting minutes, back issues of the
guild magazine. They live under `site/content-members/`, in a top-level section
per language, `content-members/fi/jasenille/` and `content-members/en/members/`.
Otherwise they are ordinary pages, with the same front matter, the same
`translationKey` pairing and the same templates.

The build produces two trees:

```
cd site
hugo --minify --cleanDestinationDir                        # -> site/public/
hugo --minify --cleanDestinationDir --environment members  # -> site/public-members/
./check-trees.sh
```

`site/public/` is served to anyone. `site/public-members/` is copied to a
directory outside the public web root and read only after a Keycloak login, so a
member page has no public address to guess. The public build reads only
`content/`, so a member page is never loaded into it: it is absent from the page
list, from `sitemap.xml` and from anything else generated out of loaded pages.

`check-trees.sh` fails the build if a member section appears in the public tree,
is missing from the member tree, or has a page below it named anywhere inside
the public tree. It also fails if the member tree names the counting endpoint,
which nothing in that tree is allowed to do. Run it after both builds, and
again once the search indexes exist; CI runs it both times.

The section landing pages themselves, `/fi/jasenille/` and `/en/members/`, are
the exception in HTML: the header's sign-in button points at them, so the public
tree names them on every page. Caddy gates those addresses and answers them with
a redirect to Keycloak whether or not the tree it is serving holds the page, so
what is public is the door rather than anything behind it.

The public search index gets no such exception, and the second run is where
that is decided. It holds the index to the bare section path rather than to a
path with a page below it: the sign-in link sits in the header, outside the
part of the page that gets indexed, so no correct build puts a member address
there, and an address that is only a door in HTML would be a description of the
member material itself once it is in the index. The second run also proves that
every member section got its own bundle, that no bundle sits at the member
tree's root — where `/pagefind/` is an address the public tree owns — and that
the public index holds exactly as many pages as the public build marked for
indexing.

Neither build passes `--gc`. The two share one resource cache under
`site/resources/`, and a garbage collecting pass deletes the cached image
variants belonging to the other one.

Both builds must start from a clean state. Hugo does not reliably delete output
it no longer produces, and `--cleanDestinationDir` removes stale pages but not
stale published resources, so deploys copy with `rsync --delete`.

## Previews

Every pull request gets a browsable copy of both trees at
`https://pr-<number>.preview.prodeko.org/`, behind a Prodeko login that
requires the administrator role. An editor saving a draft sees the link on
their entry in the editing screen and can look at the change before it is
published.

The preview is rebuilt on every push to the branch and deleted when the pull
request is merged or closed; the server also removes previews older than a
fortnight and keeps at most fifteen. Pull requests from forks get no preview,
because GitHub does not give them the deploy key.

A preview carries the member tree as well as the public one, since the whole
host is behind the login. It carries no editing screen: `/admin` is removed
from the build, because a Decap that loads on a preview host would offer a
sign-in that cannot complete.

`.github/workflows/preview.yml` builds and publishes it; the
`prodeko_preview` and `preview_gate` roles in infra-prodeko serve it.

Templates can tell the two apart through `site.Params.members`, which is set in
the member build and in local preview and unset in the public build. A
navigation entry naming a page *below* a member section belongs behind that
condition: `data/megamenu.yaml` marks such an entry with `members: true`, and
the header renders it as plain text where there is no member tree. The
condition also carries the sign-in button's wording, which offers a login in
the public build and names the destination in the other two, where the reader
is already past the gate.

## Counting visitors

The public tree counts pageviews with [GoatCounter](https://www.goatcounter.com/)
running on Prodeko's own server: pageviews per path, referrer, language,
country, device class, and a click event on each "Avaa palvelu" button. It
stores nothing on a visitor's device, which is why the site carries no cookie
banner; the privacy notice describes what is recorded and carries the opt-out
button. The member tree is not counted at all, and `check-trees.sh` asserts it.

The dashboard is at [analytics.prodeko.org](https://analytics.prodeko.org),
behind a Prodeko sign-in that requires the `prodeko-org-admin` role. The
deployment lives in the infra-prodeko repository, in the `goatcounter` and
`analytics_gate` roles;
[the design document](docs/superpowers/specs/2026-09-19-analytics-design.md)
explains what is measured and why there is no banner.

## Checking the site

`hugo` exits zero on a page nothing links to, a link to a page that no longer
exists, and a page wider than a phone. The audit catches all three:

```
hugo -s site --environment development
python3 -m http.server 1313 --directory site/public-preview &
node tools/audit-site.mjs http://localhost:1313 site/public-preview
```

It needs `playwright`, and it runs in CI against the built site.
[docs/content-audit.md](docs/content-audit.md) is what it found on the migrated
content.

`tools/fix-front-matter.py` quotes front-matter values containing a colon,
which YAML reads as a nested mapping and Hugo fails the whole build over. Run
it after a bulk edit; `--check` reports without writing.
