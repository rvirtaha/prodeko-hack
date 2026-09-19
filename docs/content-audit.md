# Content audit

Every page of the migrated site was crawled, rendered at a phone width and
checked against the source in git. This records what was wrong, what changed,
and what is deliberately still open.

Run the audit yourself:

```bash
hugo -s site --environment development
python3 -m http.server 1313 --directory site/public-preview &
node tools/audit-site.mjs http://localhost:1313 site/public-preview
```

It exits non-zero on a broken internal link, an orphaned page, a page wider
than a phone, or a broken image.

## Where it started and where it is

| | before | after |
|---|---|---|
| pages reachable by crawling | 75 of 112 | 117 of 117 |
| broken internal links | 0 | 0 |
| pages 404ing that the menu linked to | 2 | 0 |
| pages wider than 390px | 5 | 0 |
| broken images | 1 (on 5 pages) | 0 |
| board years in the archive | 3 | 59 |
| h6 headings left over from the scrape | 264 | 0 |
| pages paired across both languages | 52 | 57 |
| pages with a description | 0 | all 117 |
| console errors | 4 distinct | 0 |

The deployable public tree, which omits the four member pages, comes out at
113 of 113 reachable with the same zero counts. The audit now runs in CI.

## What was wrong

The plain-page and section templates rendered a bare heading and the page
body with no container. Around sixty migrated pages — the majority of the
site — ran edge to edge at any window width, with no left margin and no
readable line length. This was the single largest problem and is what the
first commit fixes.

On a phone the header collapsed into a four-row stack about 220px tall,
because the rule hiding the language and login block was overridden by a
later rule of equal specificity further down the stylesheet. Worse, the
mega-menu opens on hover, which touch never fires, and was hidden outright
below the breakpoint. Roughly thirty pages were only ever linked from the
mega-menu, so on a phone they had no address a reader could reach. The
mega-menu links are now repeated as accordions in a drawer.

Two nested sections had no `_index.md`. Hugo generates a section page for a
top-level directory without one but not for a nested directory, so
`/fi/guild/guild-rules/` and `/en/guild/rules/` were 404s — and the mega-menu
linked past them, straight to the first document. This is the guild-rules
page that prompted the audit.

Thirty-seven built pages had no inbound link from anywhere: both ESTIEM
sections, the services pages, both privacy notices, the previous-boards
archive, the decrees and guidelines, and several section indexes. The footer
contributed to this — it iterated `navigation.yaml` looking for a `children`
key that file has never had, so it rendered four headings and no links at
all. It now reads `data/footer.yaml`.

The hero texture was referenced from the stylesheet as `/images/brand/
bg-texture-blue.png` but lives under `assets/`. Hugo only publishes an asset
whose address a template resolves, and no template mentioned this one, so
every blue hero 404'd behind its background colour. The address is now handed
to the stylesheet as a custom property from `head.html`, which publishes it.

The scrape flattened the heading hierarchy: 264 `######` headings against
almost no `##`, because the old site styled h6 as its subheading. Subheadings
rendered smaller than the body text around them. Heading levels were renumbered
across every section into an actual outline.

The service embeds send `frame-ancestors 'self'`, so a browser refuses to
paint ilmo.prodeko.org inside this origin. The events page in both languages
was a blank rectangle. The embed now carries a card naming the service with a
link to it and a placeholder behind the frame, so the page reads the same
whether the frame paints or is refused.

The alumni newsletter archive is 86 consecutive paragraphs each holding one
link. As paragraphs they rendered 20px tall with nothing between them, which
is neither readable nor tappable on a phone. A paragraph whose whole content
is a link is now a row in a list of documents.

## What a crawl cannot see

Every page was also rendered at 390px and read as a person holding a phone
would. That pass found things the crawler has no opinion about, and each
finding was then handed to a second agent told to refute it by loading the
page and measuring: 72 findings, 49 taken seriously, 39 confirmed.

The heaviest were in the stylesheet. A padding shorthand on an element that is
also a `.container` replaces the container's horizontal gutter with zero, and
both hero inners and both footer strips did it — so the hero kicker read
"RITYKSILLE", the h1 lost the left half of its Y, and the partner logos sat
hard against the screen edge on every page. The hero scrims are pseudo-elements
of the hero while the photo is a positioned child of it, so the photo painted
over them and white lead text was laid straight onto sunlit grass. `--text-muted`
measured 3.4:1 against the page, under AA, and it carries every page lead, card
blurb, breadcrumb and person's role.

The rest was markup the scrape had flattened:

- Section titles left as ordinary paragraphs, so pages thousands of pixels long
  had no visible structure — the eleven jaos names on the officials page, the
  seven chapter titles in the guild rules, the statute's section markers.
- Rosters written as space-indented continuation lines, which markdown folds
  into a single paragraph. Eighteen years of alumni boards read as one unbroken
  run of names and addresses.
- Literal markdown showing through: a lone `**` as its own paragraph, bold
  labels glued to the following word, `4.**Co-create**` breaking a numbered
  list, and list items written `- • Thing` that rendered with two bullets.
- Both English privacy notices were entirely in Finnish.
- Empty link targets — `[040 659 2055]()` is a link back to the current page,
  and `![]()` emits `<img src="">`, which a browser resolves to the page URL
  and fetches a second time.

The five guild values are artwork with the copy baked in. In the grid that copy
renders at about six pixels, and on the Finnish page it is in English anyway,
so the section said nothing to anyone; the guild's own words are front matter
now. The footer came to about 1500px on a phone — three screens of it under
every page — and collapses to 727px.

## Content that was wrong rather than missing

Found while sweeping, and fixed:

- A harassment contact person's address linked to a different person's
  mailbox: `[aino.soinio@aalto.fi](mailto:suvi.rinkineva@aalto.fi)`.
- Two meeting-minutes links displayed the 2021 and 2022 PDF addresses while
  pointing at the 2024 one. All three PDFs exist.
- The billing page's title was two headings concatenated by the scrape:
  "Tuotantotalouden Kilta Prodeko ry:n laskutustiedotYhdistyksen tiedot".
- The English Studies section had two pages sharing one translation key, one
  of them an older duplicate naming Oodi, which Aalto has retired in favour
  of Sisu. The newer text is kept and the retired address survives as an alias.
- A CMS edit URL had leaked into the content:
  `new.prodeko.org/...?edit&language=en`.
- A malformed mailto: `https://mailto:tarja.timonen@aalto.fi,/`.
- Links to `djangocms.prodeko.org`, `studyguides.aalto.fi`, `pora.ayy.fi`,
  `varjoopintoopas.fi` and `new.abb.com`, all retired hosts.
- A link labelled Sisu pointing at Oodi, and a MyCourses address split so its
  last letter fell outside the link: `[https://mycourses.aalto.f](...)i.`
- Seven pages carrying the old site's all-caps display styling as their actual
  title. Two of them were titled OPINNOT — the section's name rather than the
  page's — so the sidebar listed the same entry twice. The alumni register
  notice was titled in Finnish on the English page.
- The honours page was titled Kunniamaininnat, honourable mentions, on a page
  listing honorary members and the Pro Prodeko badge.

Left alone deliberately: hosts that answer a headless browser with 403 but
serve a real one fine (aalto.fi, hsl.fi, nokia.com, ayy.fi, reittiopas.fi),
and Prodeko's own services, which are not resolvable from a build machine.

## The board archive

`site/data/boards/` held three years while the page said "boards from 1967
onward". `tools/import-boards.py` reads the 58 collapsible panels off the live
page and writes one data file per year: 1967 to 2025, 595 people. 1994 is
absent from the source too, so it is absent here.

This is the one-off parse [the roadmap](roadmap.md) describes as item E, and
it is what makes a year-archive template worth having over a hand-edited page.
The general converter from the Django dump is still item H.

## Language parity

57 pages are paired across Finnish and English. Two exist only in Finnish:
the alumni newsletters and the orientation week page. None exist only in
English.

The five English Studies pages were written during this audit, translated from
their Finnish originals, because four separate entries in the English mega-menu
all pointed at the same page for want of them.

Where a page has no counterpart the language switch falls back to the other
language's front page rather than disappearing, which would otherwise leave a
reader with no way across.

## Still open

- `ilmo.prodeko.org` cannot be framed. The fallback card is the fix available
  from this side; removing the CSP or serving the embed from a subdomain of
  prodeko.org is the fix available from theirs.
- `data/officials/` holds one year against the boards' 59. The full officials
  list runs to well over a hundred people per year and belongs with the
  Django-dump converter.
- `/admin/` is Decap's own interface and is not responsive. Editors use it on
  a laptop. The audit skips it for that reason.
- `www.abb.com` is not reachable from this build machine at all, so the
  replacement for the retired `new.abb.com` is unverified.
- Two pages exist only in Finnish: the alumni newsletters and orientation week.
- `site/content/fi/guild/board/_index.md` has a bullet the scrape truncated —
  "Teekkarikulttuuritoimikunnassa (TKTMK)" with no verb. It wants a human with
  the live page open, not a guess.
- The values artwork still carries its baked-in copy under the readable text.
  Cropping it out is image work, not CSS.
