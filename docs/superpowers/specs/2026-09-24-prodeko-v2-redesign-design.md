# Prodeko.org v2 visual redesign

## Summary

The site built by the [git-CMS roadmap](../../roadmap.md) is complete and
carries the v1 look: blue hero with rainbow stripe, kicker labels, announcement
cards, a full-width title band, text-only people grids. The design handoff
(the `design_handoff_prodeko_site` bundle beside this repo) and the
Prodeko Design System skill define v2: a white, text-first site with radius 0,
no cards, three blues, one photo per page, and a 750px content column beside a
240px sticky sidebar.

This spec covers recreating v2 in the Hugo site. The handoff mocks 11 pages;
the site has roughly 80 across Finnish, English and the Swedish abi pages, so
most of the work is applying the v2 language consistently to pages the mockup
never drew, without losing a page, a URL or a word of content.

Sources of truth, in order: the updated design system in
`.claude/skills/prodeko-design-system` (tokens, `components/prodeko.css`,
assets), then the handoff README for anything the DS does not yet carry.

## Goals

- Every page renders in the v2 design language, pixel-accurate to the mockup
  where the mockup covers it.
- All existing pages, URLs and content are preserved, including the member
  build (`site.Params.members`), the preview build and the `/se/` pages.
- The Decap editing screen stays truthful: every field the new templates read
  is editable when the redesign lands.
- The 2026 board members get real headshots.

Non-goals: satellite services, content rewrites beyond what the handoff
specifies verbatim, migrating photos for past boards or officials, the
handoff's "not yet designed" ideas (collapsed section nav under 900px, mobile
front-page sidebar placement, horizontal ArchiveJump).

## Decisions already made

- Current board and officials keep a grid presentation, restyled to v2 and
  extended with photos where data provides them. Archive years render as
  definition lists per the mockup.
- Decap config is updated in the same effort.
- The spare DS photos (dock-jump, wappu, fuksi-group) are assigned to fitting
  hub pages beyond the five the handoff names.
- Work happens on `redesign/v2`, branched from `main`.

## Design

### Tokens and CSS

`site/assets/css/main.css` is rewritten against the DS v2
`components/prodeko.css` and the handoff's token values (§11). Token files
under `site/assets/css/tokens/` are synced with the DS versions. Dead v1
vocabulary (kickers, announcement cards, inverse buttons, hero gradients,
non-zero radii, shadows outside the drop-down panels) is deleted, not kept for
compatibility. The existing JS (`mega-menu.js`, `search.js`) is kept and
adjusted, not rewritten.

### Data layer

The chrome reads data files, so the IA change is mostly data:

- `navigation.yaml`: Fukseille becomes a top-level item with its own menu,
  Tapahtumat leaves the bar. FI order: Kilta, Abeille, Yrityksille,
  Alumneille, Fukseille, then the search toggle. EN mirrors.
- `megamenu.yaml`: columns per handoff §3. Kilta gains Opinnot and Palvelut
  columns, Fukseille gets its own single-menu column set, Abeille gains the
  language column linking `/se/`.
- `sectionnav.yaml` (new): one nav list per section per language, from the
  handoff's SNAV for mocked sections and written by hand for the rest
  (ESTIEM, säännöt, tietoa alumnista, member pages). The sidebar and the
  option-C phone menu groups read it.
- `news.yaml` (new): the front page Ajankohtaista list, entries of
  title, url, description per language.
- `footer.yaml`: restructured per handoff §8.
- `boards/2026.yaml`: each member gains a `photo` field.

### Aside blocks

Pages carry sidebar content as frontmatter, rendered by one partial:

```yaml
aside:
  - title: Kiltahuone
    text: |
      ...
  - title: Jäsenyys
    text: Jäsenyys maksaa 8 € lukuvuodessa.
    button: { label: Hae jäseneksi, url: "https://membership.prodeko.org/apply" }
  - title: Ajankohtaista
    links: [{ label: ..., url: ..., description: ... }]
```

Three shapes cover the whole handoff: text, text with one button, link list.
A page's sidebar is SectionNav first, then its aside blocks. Contact details
appear only in asides the handoff assigns and in the footer.

### Global chrome

- Header: 64px white sticky bar, `emblem-blue.svg` at 36px, centred plain-link
  nav with the current-item inset underline, FI/EN switch and a small Kirjaudu
  button. Current-item logic per handoff §2. The existing member-build label
  logic is kept.
- Mega-menu: same open-on-hover machinery, restyled shell (4px rainbow strip,
  hairline, shadow), caps column titles, arrow rows, members-only links
  disabled in the public build as today.
- Phone menu: the two-level details accordion is replaced by option C. Search
  row on top, ETUSIVU, one group per menu with section-hub sub-links from
  `sectionnav.yaml`, direct links for the rest, lang switch and a full-width
  Kirjaudu at the bottom. One group open at a time, the current page's group
  pre-opened. Deep links that leave the phone menu remain reachable through
  each page's SectionNav.
- Search: Pagefind stays. The panel and field are restyled per §5, and
  behavior is aligned: Enter opens the top result, a count line in the
  handoff's wording, a clear button in the phone menu. The existing combobox
  accessibility and member-index handling are kept.
- Footer: rainbow rule on top (the page's only one), address block, Linkit
  column, partner logos, bottom row with the copyright and social links.

### Templates

- Page frame: `page-shell.html` becomes the v2 frame. Main column is
  Breadcrumbs + H1, an optional Figure, then Prose. The full-width title band
  and its rainbow rule go away. Sidebar holds SectionNav and the page's aside
  blocks; below 900px it stacks under the content.
- Front page: HomeHero in the default no-links variant over the guild photo,
  main column per handoff §6, sidebar with Ajankohtaista from `news.yaml` and
  the Jäsenyys block.
- People pages: the people grid keeps name and role and renders a square
  headshot when the entry has a `photo`, radius 0, no card box. Archive years
  in `archive.html` become role → name definition lists with Puheenjohtaja
  first, under an ArchiveJump year list with 88px scroll offset.
- Hub layouts: the five bespoke hub layouts collapse into the standard frame
  where the mockup shows hubs as ordinary content pages; layouts that remain
  (people, archive, redirect) are restyled in place.

### Board photos

The 12 photos in the `board-photos/` directory beside this repo are copied to
`site/assets/images/board/2026/` as `firstname-lastname.jpg`, referenced from
`boards/2026.yaml`, resized through Hugo's image pipeline, and editable in
Decap through the new photo field. Names match the 12 current members
one-to-one.

### Content adaptation rules

For pages the mockup does not draw: the standard frame applies, the section's
nav comes from `sectionnav.yaml`, asides are assigned per section (a contact
aside only where the handoff gives one), and copy is untouched. Where the
handoff specifies copy verbatim (front page, Tervetuloa tutalle) the content
files are checked against it. Spare photos go to Fukseille and ESTIEM hub
pages; everything else stays text-only until real photos exist.

## Implementation plan

Phases run in order; each ends in a commit and a review.

- Phase 0, groundwork: branch, assets copied in (emblem, photos, board
  headshots renamed), tokens synced, data files rewritten
  (`navigation`, `megamenu`, `sectionnav`, `news`, `footer`, board photos).
- Phase 1, foundation and chrome, sequential: `main.css` rewrite, header,
  mega-menu, phone menu, search, footer. One CSS file and one header partial;
  parallel agents would collide.
- Phase 2, templates: page frame, front page, people grid, archive, hub
  collapse.
- Phase 3, content adaptation, workflow fan-out: one agent per section
  applying frontmatter and asides, plus the Decap config update.
- Phase 4, verification, workflow fan-out: build public and member variants,
  screenshot every page at 1280 and 390 against the served prototype, check
  every pre-existing URL resolves, run the fix loop until dry.

## Risks

- Adaptation judgment: 70 pages have no mockup. The sectionnav data file and
  the three-shape aside vocabulary bound the judgment calls; anything that
  does not fit those is raised rather than invented.
- The option-C phone menu removes deep links from the menu itself. It relies
  on every deep page being reachable via a section hub's SectionNav, which
  `sectionnav.yaml` must guarantee for every section, including off-mockup
  ones.
- The member build renders chrome from the same data; both variants are built
  and checked in phase 4, not just the public one.
- Editor commits land on `main` while this branch lives. Content files are
  mostly untouched by phases 0–2, so rebases stay cheap; phase 3 touches
  frontmatter and should land promptly after review.
