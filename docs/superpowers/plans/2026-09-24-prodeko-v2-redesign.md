# Prodeko.org v2 Visual Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. This run executes phases 3 and 4 as Workflow fan-outs per the execution note at the end.

**Goal:** Recreate the v2 design (white, text-first, 750+240 grid) across every page of the Hugo site without losing a page, URL, or word of content.

**Architecture:** The chrome and page frame read data files (`navigation`, `megamenu`, `sectionnav`, `news`, `footer`) and frontmatter (`photo`, `aside`), so most of the redesign is one CSS rewrite, five partials, and data. Content adaptation then fans out per section.

**Tech Stack:** Hugo v0.166 extended, Pagefind (existing), Decap CMS, Playwright (`tools/audit-site.mjs`).

**Spec:** `docs/superpowers/specs/2026-09-24-prodeko-v2-redesign-design.md`

## Global Constraints

- Sources of truth in order: `.claude/skills/prodeko-design-system` (tokens, `components/prodeko.css`, README rules), then the handoff `../design_handoff_prodeko_site/README.md` (§ references below point there), then this plan.
- No existing URL changes. Content files are edited in place, never renamed or moved.
- Both builds must stay green at every commit: `hugo -s site --minify --cleanDestinationDir` and the same with `--environment members`. Development env builds `public-preview`.
- v2 design law: Raleway only, radius 0, no cards, no shadows except mega/search panels, colour transitions 140ms only, no other animation. One photo per page, one primary button per page, contact details only in footer + assigned asides.
- Breakpoints: nav/phone menu at 1000px, page grid collapse at 900px, def-list collapse at 600px.
- Docs and comments describe the present. Never write "now uses", "previously", "moved from", or any change narration in code comments, docs, or content.
- Commit messages in the repo's plain-sentence style (`git log --oneline` for examples), each ending with the Co-Authored-By trailer.
- Editors must be able to edit every field the new templates read (Task 22).
- Subagents run on `opus` (or `sonnet` for mechanical steps); never fable.

## Review Focus

Failure modes the spec implies but no mockup shows; each has a pinned test in the named task:

1. A page with no sectionnav match and no asides (e.g. `/fi/tietosuoja/`) must render the single-column `no-aside` grid, not an empty sticky sidebar. — Task 12.
2. The members build must render members-only menu links as real links and the public build as disabled text; the member search index attribute must survive the header rewrite. — Task 8.
3. A `photo` path that doesn't resolve (typo in `boards/2026.yaml` or frontmatter) must fail the build loudly, not render a hole. — Tasks 12 and 14.
4. The `/sv/` pages live in the fi language tree with overridden URLs; header current-item logic and sectionnav prefix matching must not crash or mis-highlight there. — Task 16.
5. The option-C phone menu drops ~30 deep links from the menu; every page must remain reachable through hub pages' SectionNavs (orphan check). — Task 23.

---

## Phase 0 — groundwork

### Task 1: Bring in the v2 assets

**Files:**
- Create: `site/assets/images/brand/emblem-blue.svg` (copy)
- Create: `site/assets/images/pages/page-guild-crop.png`, `page-prospective-crop.png`, `dock-jump.jpg`, `fuksi-group-steps.jpg`, `hero-wappu-flag.jpg`, `wappu-group-kaivopuisto.jpg` (copies)
- Create: `site/assets/images/board/2026/*.jpg` (12 renamed copies)

**Interfaces:**
- Produces: asset paths used by Tasks 8 (emblem), 13 (hero), 14 (board photos), 15–19 (page photos).

- [ ] **Step 1: Copy brand and page photos**

```bash
cd /home/rvirt/code/prodeko-hack/main
DS=.claude/skills/prodeko-design-system/assets
cp $DS/logos/emblem-blue.svg site/assets/images/brand/
cp $DS/photos/page-guild-crop.png $DS/photos/page-prospective-crop.png \
   $DS/photos/dock-jump.jpg $DS/photos/fuksi-group-steps.jpg \
   $DS/photos/hero-wappu-flag.jpg $DS/photos/wappu-group-kaivopuisto.jpg \
   site/assets/images/pages/
```

- [ ] **Step 2: Copy board photos under clean names**

```bash
mkdir -p site/assets/images/board/2026
B=../board-photos; S=__400x400_q85_ALIAS-hallitus_crop_subsampling-2.jpg
cp "$B/aaron_katainen.jpg$S"  site/assets/images/board/2026/aaron-katainen.jpg
cp "$B/kalle_kinnala.jpg$S"   site/assets/images/board/2026/kalle-kinnala.jpg
cp "$B/arttu_kaitila.jpg$S"   site/assets/images/board/2026/arttu-kaitila.jpg
cp "$B/emilia_schmidt.jpg$S"  site/assets/images/board/2026/emilia-schmidt.jpg
cp "$B/tuomas_ylimaki.jpg$S"  site/assets/images/board/2026/tuomas-ylimaki.jpg
cp "$B/touko_heinila.jpg$S"   site/assets/images/board/2026/touko-heinila.jpg
cp "$B/elina_lauri.jpg$S"     site/assets/images/board/2026/elina-lauri.jpg
cp "$B/aida_sinkkonen.jpg$S"  site/assets/images/board/2026/aida-sinkkonen.jpg
cp "$B/helmi_halinen.jpg$S"   site/assets/images/board/2026/helmi-halinen.jpg
cp "$B/joona_isosaari.jpg$S"  site/assets/images/board/2026/joona-isosaari.jpg
cp "$B/lauri_porsti.jpg$S"    site/assets/images/board/2026/lauri-porsti.jpg
cp "$B/daniel_nikkar.jpg$S"   site/assets/images/board/2026/daniel-nikkar.jpg
```

- [ ] **Step 3: Verify and build**

Run: `ls site/assets/images/board/2026 | wc -l` → expected `12`.
Run: `hugo -s site --environment development` → expected exit 0.

- [ ] **Step 4: Commit** — `git add site/assets/images && git commit`

### Task 2: Sync design tokens

**Files:**
- Modify: `site/assets/css/tokens/{fonts,colors,typography,spacing,radius,elevation,motion,base}.css` (replace with DS versions)

**Interfaces:**
- Produces: custom properties consumed by the Task 7 stylesheet: `--pd-blue-dark #002851`, `--pd-blue #002e7d`, `--pd-blue-light #0053a0`, `--pd-black #000a14`, `--border-subtle`, `--surface-sunken`, `--pd-blue-tint-08`, `--text-muted`, `--content-w 750px`, `--aside-w 240px`, `--rainbow-gradient`, `--shadow-md`, sizes/weights/spacing.

- [ ] **Step 1: Replace each token file with the DS copy**

```bash
for f in fonts colors typography spacing radius elevation motion base; do
  cp .claude/skills/prodeko-design-system/tokens/$f.css site/assets/css/tokens/$f.css
done
```

Do NOT copy `tokens/assets.css` — its `url()` paths are DS-relative. Image custom properties are injected by `head.html` in Task 7.

- [ ] **Step 2: Check every property `components/prodeko.css` uses exists**

Run: `grep -o 'var(--[a-z0-9-]*' .claude/skills/prodeko-design-system/components/prodeko.css | sort -u | sed 's/var(//' | while read p; do grep -qr -- "$p:" site/assets/css/tokens/ || echo "MISSING $p"; done`
Expected: only `--img-*` and `--photo-*` lines (handled in Task 7). Any other MISSING line: add that property to the matching token file with the DS value.

- [ ] **Step 3: Verify the font files the DS `fonts.css` references resolve** — if `@font-face` paths differ from the site's existing Raleway locations, keep the site's `src` paths and the DS's remaining declarations.

- [ ] **Step 4: Build both envs, commit** — builds will look broken (old main.css against new tokens is fine mid-phase as long as `hugo` exits 0).

### Task 3: Navigation and mega-menu data

**Files:**
- Modify: `site/data/navigation.yaml` (replace lists, keep the file's header comment style)
- Modify: `site/data/megamenu.yaml` (move one column per language)

**Interfaces:**
- Produces: nav item fields `title, url, menuKey, owns (list), mobileChildren (list of {label,url})` consumed by Tasks 8 and 9. Mega key `fuksit` in both languages.

- [ ] **Step 1: Replace the per-language lists in `navigation.yaml`**

```yaml
fi:
  - title: Kilta
    url: /fi/guild/
    menuKey: guild
    owns: [/fi/guild/, /fi/opinnot/, /fi/palvelut/, /fi/estiem-lg-helsinki/]
    mobileChildren:
      - { label: Kilta, url: /fi/guild/ }
      - { label: Hallitus ja toimarit, url: /fi/guild/hallitus-ja-toimihenkilot/ }
      - { label: Opinnot, url: /fi/opinnot/ }
      - { label: Palvelut, url: /fi/palvelut/ }
  - title: Abeille
    url: /fi/abit/
    menuKey: prospective
    owns: [/fi/abit/, /sv/]
  - title: Yrityksille
    url: /fi/yrityssuhteet/
    menuKey: corporate
  - title: Alumneille
    url: /fi/alumni/
    menuKey: alumni
  - title: Fukseille
    url: /fi/new-students/
    menuKey: fuksit
en:
  - title: Guild
    url: /en/guild/
    menuKey: guild
    owns: [/en/guild/, /en/studies/, /en/services/, /en/estiem-lg-helsinki/]
    mobileChildren:
      - { label: Guild, url: /en/guild/ }
      - { label: Board and officials, url: /en/guild/board-and-officials/ }
      - { label: Studies, url: /en/studies/ }
      - { label: Services, url: /en/services/ }
  - title: Prospective students
    url: /en/prospective-students/
    menuKey: prospective
  - title: Companies
    url: /en/yrityssuhteet/
    menuKey: corporate
  - title: Alumni
    url: /en/alumni/
    menuKey: alumni
  - title: New students
    url: /en/new-students/
    menuKey: fuksit
```

Rewrite the file's header comment to describe this shape (owns = page prefixes whose pages light this item; mobileChildren = the item's group rows in the phone menu; items without mobileChildren are direct links there). The Tapahtumat/Events entry is deleted; `/fi/tapahtumat/` and `/en/events/` pages remain as addresses.

- [ ] **Step 2: Move the Fukseille column to its own key**

In `megamenu.yaml` fi: cut the whole `- title: Fukseille` column (currently lines ~43–60) out of `guild` and paste it as the sole column under a new top-level key `fuksit:` in the fi map. Same in en: move `- title: New students` (~196–211) to `en.fuksit`. Update the file's comments to match. Verify with:

Run: `python3 -c "import yaml,sys; d=yaml.safe_load(open('site/data/megamenu.yaml')); [print(l, list(d[l].keys()), [c['title'] for c in d[l]['guild']]) for l in ('fi','en')]"`
Expected: both languages have keys including `fuksit`, and `guild` has 4 columns (Kilta/Guild, board, Opinnot/Studies, Palvelut/Services).

- [ ] **Step 3: Build both envs** (header still reads old fields; must not crash — `owns`/`mobileChildren` are additive), **commit**.

### Task 4: Section navigation data

**Files:**
- Create: `site/data/sectionnav.yaml`

**Interfaces:**
- Produces: per language an ordered list of `{title, match: [prefixes], items: [{label, url, members?}]}`. Consumed by the Task 12 `section-nav.html` rewrite. First entry whose prefix matches the page path wins, so more specific entries come first.

- [ ] **Step 1: Write the file.** Content below is authoritative (derived from the prototype SNAV, corrected to real repo URLs, extended to off-mockup sections):

```yaml
fi:
  - title: Hallitus ja toimarit
    match: [/fi/guild/hallitus-ja-toimihenkilot/]
    items:
      - { label: Hallitus ja toimihenkilöt, url: /fi/guild/hallitus-ja-toimihenkilot/ }
      - { label: Hallitus 2026, url: /fi/guild/hallitus-ja-toimihenkilot/hallitus/ }
      - { label: Hallituksen toiminta, url: /fi/guild/hallitus-ja-toimihenkilot/hallituksen-toiminta/ }
      - { label: Edelliset hallitukset, url: /fi/guild/hallitus-ja-toimihenkilot/edelliset-hallitukset/ }
      - { label: Toimihenkilöt 2026, url: /fi/guild/hallitus-ja-toimihenkilot/toimihenkilot/ }
      - { label: Toimarien toiminta, url: /fi/guild/hallitus-ja-toimihenkilot/toimarien-toiminta/ }
  - title: Kilta
    match: [/fi/guild/]
    items:
      - { label: Kilta, url: /fi/guild/ }
      - { label: Arvot, url: /fi/guild/arvot/ }
      - { label: Häirintäyhdyshenkilöt, url: /fi/guild/hairintayhdyshenkilot/ }
      - { label: Säännöt ja asetukset, url: /fi/guild/guild-rules/ }
      - { label: Kunnianosoitukset, url: /fi/guild/kunnianosoitukset/ }
      - { label: Jäseneksi, url: /fi/guild/jaseneksi/ }
      - { label: Hallitus ja toimihenkilöt, url: /fi/guild/hallitus-ja-toimihenkilot/ }
      - { label: Fukseille, url: /fi/new-students/ }
      - { label: Opinnot, url: /fi/opinnot/ }
      - { label: Palvelut, url: /fi/palvelut/ }
  - title: Fukseille
    match: [/fi/new-students/]
    items:
      - { label: Fukseille, url: /fi/new-students/ }
      - { label: Tervetuloa tutalle!, url: /fi/new-students/welcome-to-prodeko/ }
      - { label: Fuksiopas 2026, url: /fi/new-students/fuksiopas/ }
      - { label: Orientaatioviikko 2026, url: /fi/new-students/orientaatio/ }
      - { label: Opiskelijaelämän ABC, url: /fi/new-students/studentlife/ }
      - { label: Ensimmäisen vuoden opinnot, url: /fi/new-students/ensimmaisen-vuoden-opinnot/ }
      - { label: Asuminen ja eläminen, url: /fi/new-students/asuminen-ja-elaminen/ }
      - { label: Fuksipisteet, url: /fi/new-students/fuksipisteet/ }
  - title: Opinnot
    match: [/fi/opinnot/]
    items:
      - { label: Opinnot, url: /fi/opinnot/ }
      - { label: Keneen otan yhteyttä, url: /fi/opinnot/keneen-otan-yhteytta/ }
      - { label: Usein kysyttyä opinnoista, url: /fi/opinnot/usein-kysyttya-opinnoista/ }
      - { label: Kokouskuulumisia, url: /fi/opinnot/kokouskuulumisia/ }
      - { label: Tohtoriopinnoista, url: /fi/opinnot/tohtoriopinnoista/ }
      - { label: Tärkeitä linkkejä, url: /fi/opinnot/tarkeita-linkkeja/ }
  - title: Palvelut
    match: [/fi/palvelut/]
    items:
      - { label: Palvelut, url: /fi/palvelut/ }
      - { label: Aallon ja AYY:n palvelut, url: /fi/palvelut/aallon-ja-ayyn-palveluita-2/ }
      - { label: Prodeko-rahasto, url: /fi/palvelut/prodeko-fund/ }
      - { label: ESTIEM LG Helsinki, url: /fi/estiem-lg-helsinki/ }
      - { label: Kokouspöytäkirjat, url: /fi/jasenille/poytakirjat/, members: true }
  - title: Abeille
    match: [/fi/abit/]
    items:
      - { label: Moikka abi!, url: /fi/abit/ }
      - { label: Abimentorointi, url: /fi/abit/abimentorointi/ }
      - { label: Opiskelijoiden polkuja tutalle, url: /fi/abit/opiskelijoiden-polkuja-tutalle/ }
      - { label: Menestyneet tutalaiset, url: /fi/abit/menestyneet-tutalaiset/ }
      - { label: Opinnot, url: /fi/opinnot/ }
      - { label: Tärkeitä linkkejä, url: /fi/opinnot/tarkeita-linkkeja/ }
  - title: Yrityksille
    match: [/fi/yrityssuhteet/]
    items:
      - { label: Yrityssuhteet, url: /fi/yrityssuhteet/ }
      - { label: Tietoa yrityksille, url: /fi/yrityssuhteet/yrityksille/ }
      - { label: Yritysvierailut, url: /fi/yrityssuhteet/yritysvierailut/ }
      - { label: Rekrytointi, url: /fi/yrityssuhteet/rekrytointi/ }
      - { label: Prodeko Network, url: /fi/yrityssuhteet/prodeko-network/ }
      - { label: aTalent, url: /fi/yrityssuhteet/atalent/ }
      - { label: Laskutustiedot, url: /fi/yrityssuhteet/laskutustiedot/ }
  - title: Alumni
    match: [/fi/alumni/]
    items:
      - { label: Alumni, url: /fi/alumni/ }
      - { label: Tietoa alumnista, url: /fi/alumni/tietoa-alumnista/ }
      - { label: Alumnitiedotteet, url: /fi/alumni/tietoa-alumnista/alumnitiedotteet/ }
      - { label: Alumnin hallitukset, url: /fi/alumni/tietoa-alumnista/alumnin-hallitukset/ }
      - { label: Alumnin säännöt, url: /fi/alumni/tietoa-alumnista/alumnin-saannot/ }
      - { label: Neuvottelukunta, url: /fi/alumni/neuvottelukunta/ }
      - { label: Lifelong Learning, url: /fi/alumni/lifelong-learning/ }
      - { label: Prodeko Ventures, url: /fi/alumni/prodeko-ventures/ }
  - title: ESTIEM LG Helsinki
    match: [/fi/estiem-lg-helsinki/]
    items:
      - { label: ESTIEM LG Helsinki, url: /fi/estiem-lg-helsinki/ }
      - { label: Mikä on ESTIEM?, url: /fi/estiem-lg-helsinki/mika-estiem/ }
      - { label: ESTIEM-tapahtumat, url: /fi/estiem-lg-helsinki/estiem-events/ }
      - { label: Tapahtumat Helsingissä, url: /fi/estiem-lg-helsinki/tapahtumat-helsingissa/ }
      - { label: Haku tapahtumiin, url: /fi/estiem-lg-helsinki/haku-tapahtumiin/ }
      - { label: Kokemuksia ja matkakertomuksia, url: /fi/estiem-lg-helsinki/kokemuksia/ }
  - title: Abiturienter
    match: [/sv/]
    items:
      - { label: Abiturienter, url: /sv/ }
      - { label: Studerandenas vägar, url: SV_URL_1 }
      - { label: Abimentorskap 2026, url: SV_URL_2 }
      - { label: Framgångsrika produktionsekonomer, url: SV_URL_3 }
  - title: Jäsenille
    match: [/fi/jasenille/]
    items:
      - { label: Jäsenille, url: /fi/jasenille/ }
      - { label: Kokouspöytäkirjat, url: /fi/jasenille/poytakirjat/ }
en:
  - title: Board and officials
    match: [/en/guild/board-and-officials/]
    items:
      - { label: Board and officials, url: /en/guild/board-and-officials/ }
      - { label: Board 2026, url: /en/guild/board-and-officials/board/ }
      - { label: What the board does, url: /en/guild/board-and-officials/what-the-board-does/ }
      - { label: Previous boards, url: /en/guild/board-and-officials/previous-boards/ }
      - { label: Officials 2026, url: /en/guild/board-and-officials/officials/ }
      - { label: What the officials do, url: /en/guild/board-and-officials/what-the-officials-do/ }
  - title: Guild
    match: [/en/guild/]
    items:
      - { label: Guild, url: /en/guild/ }
      - { label: Values, url: /en/guild/values/ }
      - { label: Harassment contact persons, url: /en/guild/harassment-contact-persons/ }
      - { label: Rules and regulations, url: /en/guild/rules/ }
      - { label: Honours, url: /en/guild/honour/ }
      - { label: Joining the guild, url: /en/guild/joining-guild/ }
      - { label: Board and officials, url: /en/guild/board-and-officials/ }
      - { label: New students, url: /en/new-students/ }
      - { label: Studies, url: /en/studies/ }
      - { label: Services, url: /en/services/ }
  - title: New students
    match: [/en/new-students/]
    items:
      - { label: New students, url: /en/new-students/ }
      - { label: Welcome to IEM, url: /en/new-students/welcome-to-prodeko/ }
      - { label: Survival Guide, url: /en/new-students/survival-guide/ }
      - { label: Student life ABC, url: /en/new-students/studentlife/ }
      - { label: First-year studies, url: /en/new-students/first-year-studies/ }
      - { label: Housing and living, url: /en/new-students/living/ }
      - { label: Fuksi points, url: /en/new-students/freshmen-points/ }
  - title: Studies
    match: [/en/studies/]
    items:
      - { label: Studies, url: /en/studies/ }
      - { label: Who to contact, url: /en/studies/who-to-contact/ }
      - { label: Studies FAQ, url: /en/studies/studies-faq/ }
      - { label: Meeting news, url: /en/studies/meeting-news/ }
      - { label: Doctoral studies, url: /en/studies/doctoral-studies/ }
      - { label: Important links, url: /en/studies/important-links/ }
  - title: Services
    match: [/en/services/]
    items:
      - { label: Services, url: /en/services/ }
      - { label: Aalto and AYY services, url: /en/services/aalto-and-ayy-services-3/ }
      - { label: Prodeko Fund, url: /en/services/prodeko-fund/ }
      - { label: ESTIEM LG Helsinki, url: /en/estiem-lg-helsinki/ }
      - { label: Meeting minutes, url: /en/members/minutes/, members: true }
  - title: Prospective students
    match: [/en/prospective-students/]
    items:
      - { label: Hello, applicant, url: /en/prospective-students/ }
      - { label: Studies, url: /en/studies/ }
      - { label: Important links, url: /en/studies/important-links/ }
  - title: For companies
    match: [/en/yrityssuhteet/]
    items:
      - { label: Corporate relations, url: /en/yrityssuhteet/ }
      - { label: About corporate relations, url: /en/yrityssuhteet/about/ }
      - { label: Company visits, url: /en/yrityssuhteet/excursions/ }
      - { label: Recruitment, url: /en/yrityssuhteet/recruiting/ }
      - { label: Prodeko Network, url: /en/yrityssuhteet/prodeko-network/ }
      - { label: aTalent, url: /en/yrityssuhteet/atalent/ }
      - { label: Billing information, url: /en/yrityssuhteet/billing/ }
  - title: Alumni
    match: [/en/alumni/]
    items:
      - { label: Alumni, url: /en/alumni/ }
      - { label: About the alumni, url: /en/alumni/about-alumni/ }
      - { label: Alumni boards, url: /en/alumni/about-alumni/previous-alumni-boards/ }
      - { label: Alumni rules, url: /en/alumni/about-alumni/rules-prodeko-alumni/ }
      - { label: Advisory board, url: /en/alumni/honorary-advisory-council/ }
      - { label: Lifelong Learning, url: /en/alumni/lifelong-learning/ }
      - { label: Prodeko Ventures, url: /en/alumni/prodeko-ventures/ }
  - title: ESTIEM LG Helsinki
    match: [/en/estiem-lg-helsinki/]
    items:
      - { label: ESTIEM LG Helsinki, url: /en/estiem-lg-helsinki/ }
      - { label: What is ESTIEM?, url: /en/estiem-lg-helsinki/what-estiem/ }
      - { label: ESTIEM events, url: /en/estiem-lg-helsinki/estiem-events/ }
      - { label: Events in Helsinki, url: /en/estiem-lg-helsinki/events-helsinki/ }
      - { label: Applying to events, url: /en/estiem-lg-helsinki/applying-events/ }
      - { label: Experiences, url: /en/estiem-lg-helsinki/experiences/ }
  - title: Members
    match: [/en/members/]
    items:
      - { label: Members, url: /en/members/ }
      - { label: Meeting minutes, url: /en/members/minutes/ }
```

- [ ] **Step 2: Resolve the `SV_URL_*` and member-page placeholders against the real build.** Run `hugo -s site --environment development` then `find site/public-preview/sv -name index.html | sort` and `find site/public-members -path '*jasenille*' -o -path '*members*' -name index.html | sort` (after a members build). Replace `SV_URL_1..3` with the three built Swedish page URLs, and correct the `/fi/jasenille/poytakirjat/` and `/en/members/*` entries if the built paths differ. The file must contain no `SV_URL` string when this step is done.

- [ ] **Step 3: Check every non-members URL in the file exists in the built tree**

```bash
python3 - <<'EOF'
import yaml, os
d = yaml.safe_load(open('site/data/sectionnav.yaml'))
for lang, entries in d.items():
    for e in entries:
        for it in e['items']:
            if it.get('members') or it['url'].startswith(('http', 'mailto')):
                continue
            p = 'site/public-preview' + it['url'] + 'index.html'
            if not os.path.exists(p):
                print('MISSING', lang, e['title'], it['url'])
EOF
```

Expected: no output. Fix any wrong URL by checking the content tree, not by deleting the entry.

- [ ] **Step 4: Commit.**

### Task 5: News and footer data

**Files:**
- Create: `site/data/news.yaml`
- Modify: `site/data/footer.yaml` (replace wholesale)

**Interfaces:**
- Produces: `news.<lang>` = `{title, items: [{label, url, description}]}` consumed by Task 13. `footer` = `{address: [3 lines], legal, <lang>: {linksTitle, links: [{label,url}], bottom: [{label,url}]}}` consumed by Task 11.

- [ ] **Step 1: Write `news.yaml`** (editor-owned; hooked into Decap in Task 22):

```yaml
fi:
  title: Ajankohtaista
  items:
    - { label: Prodeko hack 2026, url: "https://ilmo.prodeko.org", description: Ilmoittautuminen Ilmokilkessä }
    - { label: Tervetuloa tutalle!, url: /fi/new-students/welcome-to-prodeko/, description: Fuksikapteenin tervehdys }
    - { label: Orientaatioviikko 2026, url: /fi/new-students/orientaatio/, description: Aikataulu on julkaistu }
en:
  title: News
  items:
    - { label: Prodeko hack 2026, url: "https://ilmo.prodeko.org", description: Sign up in Ilmokilke }
    - { label: Welcome to IEM, url: /en/new-students/welcome-to-prodeko/, description: "The fuksi captain's greeting" }
    - { label: New students, url: /en/new-students/, description: Autumn 2026 dates }
```

- [ ] **Step 2: Replace `footer.yaml`** per handoff §8 (single Linkit column replaces the four groups):

```yaml
address:
  - Tuotantotalouden kilta Prodeko ry
  - PL 15500, 00076 Aalto
  - TUAS-talo, Maarintie 8, 02150 Espoo
legal: "© Tuotantotalouden kilta Prodeko ry 1966–2026"
fi:
  linksTitle: Linkit
  links:
    - { label: Arvot, url: /fi/guild/arvot/ }
    - { label: Hallitus 2026, url: /fi/guild/hallitus-ja-toimihenkilot/hallitus/ }
    - { label: Edelliset hallitukset, url: /fi/guild/hallitus-ja-toimihenkilot/edelliset-hallitukset/ }
    - { label: Jäseneksi, url: /fi/guild/jaseneksi/ }
    - { label: Fukseille, url: /fi/new-students/ }
    - { label: Opinnot, url: /fi/opinnot/ }
    - { label: Abeille, url: /fi/abit/ }
    - { label: Häirintäyhdyshenkilöt, url: /fi/guild/hairintayhdyshenkilot/ }
    - { label: Kaikki palvelut, url: /fi/palvelut/ }
    - { label: Yrityksille, url: /fi/yrityssuhteet/ }
    - { label: Alumni, url: /fi/alumni/ }
    - { label: "hallitus@prodeko.org", url: "mailto:hallitus@prodeko.org" }
  bottom:
    - { label: Tietosuojaseloste, url: /fi/tietosuoja/ }
    - { label: Ylläpito, url: /admin/ }
    - { label: Instagram, url: "https://www.instagram.com/prodeko/" }
    - { label: LinkedIn, url: "https://www.linkedin.com/company/prodeko/" }
    - { label: TikTok, url: "https://www.tiktok.com/@prodeko" }
en:
  linksTitle: Links
  links:
    - { label: Values, url: /en/guild/values/ }
    - { label: Board 2026, url: /en/guild/board-and-officials/board/ }
    - { label: Previous boards, url: /en/guild/board-and-officials/previous-boards/ }
    - { label: Joining the guild, url: /en/guild/joining-guild/ }
    - { label: New students, url: /en/new-students/ }
    - { label: Studies, url: /en/studies/ }
    - { label: Prospective students, url: /en/prospective-students/ }
    - { label: Harassment contact persons, url: /en/guild/harassment-contact-persons/ }
    - { label: All services, url: /en/services/ }
    - { label: For companies, url: /en/yrityssuhteet/ }
    - { label: Alumni, url: /en/alumni/ }
    - { label: "hallitus@prodeko.org", url: "mailto:hallitus@prodeko.org" }
  bottom:
    - { label: Privacy policy, url: /en/privacy-policy/ }
    - { label: Admin, url: /admin/ }
    - { label: Instagram, url: "https://www.instagram.com/prodeko/" }
    - { label: LinkedIn, url: "https://www.linkedin.com/company/prodeko/" }
    - { label: TikTok, url: "https://www.tiktok.com/@prodeko" }
```

- [ ] **Step 3: Build (footer.html still reads the old shape — if the build errors on the new shape, guard the old partial's reads with `with`; the partial is replaced in Task 11). Commit.**

### Task 6: Board photo data

**Files:**
- Modify: `site/data/boards/2026.yaml`

**Interfaces:**
- Produces: optional member field `photo: images/board/2026/<name>.jpg` consumed by Task 14's `people-grid.html`.

- [ ] **Step 1: Add a `photo` line to each of the 12 members**, matching by name (e.g. under `- name: Aaron Katainen` add `photo: images/board/2026/aaron-katainen.jpg`). Slug = lowercase, ö→o, ä→a.
- [ ] **Step 2: Verify** — `grep -c 'photo:' site/data/boards/2026.yaml` → `12`; every referenced file exists: `grep 'photo:' site/data/boards/2026.yaml | awk '{print "site/assets/"$2}' | xargs ls -la`.
- [ ] **Step 3: Build, commit.**

---

## Phase 1 — foundation and chrome (sequential; one agent at a time)

### Task 7: Stylesheet rewrite

**Files:**
- Modify: `site/assets/css/main.css` (rewrite)
- Modify: `site/layouts/partials/head.html` (image custom properties)

**Interfaces:**
- Produces: the v2 class vocabulary consumed by every later template task, exactly as named in `components/prodeko.css`: `.container .btn .btn-primary .btn-sm .btn-block .lang-switch .site-header .nav .side .mega-panel .mega-panel-rainbow .mega-panel-inner .mega-col-title .mega-col .mega-link-disabled .nav-toggle .mobile-nav .m-list .m-row .m-marker .m-sub .m-members .m-bottom .site-footer .footer-grid .footer-title .footer-links .footer-partners .footer-bottom .page-layout .no-aside .page-aside .breadcrumbs .page-title .section-nav .section-nav-title .aside-block .aside-body .prose .lead .figure .faq-item .def-list .link-list .archive-jump .partner-logos .embed .home-hero .home-hero-content .audience-links .search-panel .search-panel-inner .search-input .is-filled .is-row .search-meta .search-count .search-clear .search-result .search-result-title .search-result-excerpt .sr-only .skip-link .rainbow-rule .people-grid .person .doc-link .search-status .search-more .archive-group`.

- [ ] **Step 1: Write the new `main.css`.** Start from a verbatim copy of `.claude/skills/prodeko-design-system/components/prodeko.css`, then:
  1. Delete `.pd-logo`/`.pd-emblem` (templates use `<img>`); keep `.rainbow-rule`.
  2. Add site-specific classes the DS file lacks, restyled to v2 values: `.people-grid` (grid `repeat(auto-fill, minmax(150px, 1fr))`, gap `var(--space-6)`; `.person` = photo (square, `width:100%`) above 15px bold name above 14px `--text-muted` role; no border, no background, radius 0), `.name-rows` row styles matching `.def-list`, `.doc-link` (row style like `.link-list a`), `.search-status`, `.search-more`, `.search-result.is-active { background: var(--pd-blue-tint-04) }`, `.archive-group h2 { scroll-margin-top: 88px }`, `.mega-panel { display:none }` / `.mega-panel.is-open { display:block }` (no animation), `.search-panel[hidden] { display:none }`, `.mobile-nav` hidden until `.nav-open`, embed/analytics-optout/redirect classes carried over from the old file if the old file styles them (check `grep -oE '^\.[a-z-]+' site/assets/css/main.css` against templates before deleting anything a template still names).
  3. Media queries per Global Constraints breakpoints; phone menu block at 1000px.
- [ ] **Step 2: Update `head.html`:** delete the `bg-texture-blue` style block; add resolved custom properties the stylesheet needs:

```html
{{ with resources.Get "images/pages/page-guild-crop.png" }}
  <style>:root{--photo-guild:url("{{ .RelPermalink }}")}</style>
{{ end }}
```

- [ ] **Step 3: Audit for orphans both ways.** Every class used in any file under `site/layouts/` must exist in the new CSS or be scheduled for removal in Tasks 8–15 (list them); every class in the new CSS must be used by the DS templates plan. Run: `grep -rhoE 'class="[^"]*"' site/layouts | tr ' "' '\n' | sort -u` and compare.
- [ ] **Step 4: Build both envs; serve and eyeball** — `devctl start hugo -- hugo server -s site --environment development --bind 0.0.0.0`; screenshot `/fi/` and `/fi/guild/` at 1280px with Playwright. Pages will look half-migrated (old markup, new CSS) — this step only confirms nothing 500s and tokens resolve.
- [ ] **Step 5: Commit.**

### Task 8: Header and mega-menu

**Files:**
- Modify: `site/layouts/partials/header.html` (bar + mega panels only; phone menu is Task 9)
- Modify: `site/assets/js/mega-menu.js` (drop footer-group code)

**Interfaces:**
- Consumes: Task 3 nav fields; Task 7 classes.
- Produces: header markup with `.site-header > .bar` (full width, no `container`), `.home` emblem link, `ul.nav`, `.nav-search-toggle`, `.side`, `.nav-toggle`; mega panels unchanged in structure.

- [ ] **Step 1: Rewrite the bar.** Emblem link: `<a class="home" href="{{ "/" | relLangURL }}"><img src=… images/brand/emblem-blue.svg … height 36 alt="Prodeko"></a>`. Nav items from `navigation.yaml`; current-item test replaces the single HasPrefix with: item is current when any prefix in `.owns | default (slice .url)` prefixes `$.RelPermalink`. Lang switch unchanged partial; the member button becomes `class="btn btn-primary btn-sm"`. Keep the skip link, `$memberHome`/`$memberLabel` logic, and the search toggle (per §2 it sits after the nav items; while the search panel is open, JS gives it the current-item styling — hook exists via `[aria-expanded="true"]` CSS).
- [ ] **Step 2: Keep the mega-panel and search-panel blocks as they are** apart from class renames Task 7 requires (none — names match). Keep `data-search-members` untouched.
- [ ] **Step 3: Trim `mega-menu.js`** — delete the footer-group section (the new footer has no `<details>`), keep panel open/close and nav-toggle wiring.
- [ ] **Step 4: Verify both variants.** Build public: `grep -o 'mega-link-disabled' site/public/fi/index.html | head -1` → present (Kokouspöytäkirjat disabled) and `grep -c 'jasenille' site/public/fi/index.html` → `0` (no member address published). Build members: `grep -c 'jasenille' site/public-members/fi/jasenille/index.html` → non-zero and no `mega-link-disabled` for that entry. `grep -c 'data-search-members' site/public-members/fi/jasenille/index.html` → `1`.
- [ ] **Step 5: Screenshot the header at 1280, hover a nav item (Playwright), compare against the prototype header. Commit.**

### Task 9: Phone menu, option C

**Files:**
- Modify: `site/layouts/partials/header.html` (`.mobile-nav` block)
- Modify: `site/assets/js/mega-menu.js` (menu behaviors)

**Interfaces:**
- Consumes: Task 3 `mobileChildren`; Task 4 sectionnav (not directly — groups come from `mobileChildren` only); Task 7 `.m-*` classes.
- Produces: markup per handoff §4: search row (Task 10's `.is-row` variant of the existing mobile search), `ETUSIVU` link, one `<details name="m-group" class="m-group">` per nav item with `mobileChildren` (summary = `.m-row` with `.m-marker` +/−), plain `.m-row` links for the rest, `.m-bottom` with lang switch and `btn btn-primary btn-block` member button.

- [ ] **Step 1: Rewrite the `.mobile-nav` block.** Order per §4. Etusivu label: `{{ if $isFi }}Etusivu{{ else }}Home{{ end }}` linking `/ | relLangURL`. Sub-links render in `.m-sub` with `aria-current="page"` on the current page. The two-level `m-sub details` accordions are gone; group content is exactly `mobileChildren`.
- [ ] **Step 2: Behaviors in JS:** `<details name="m-group">` gives one-open-at-a-time natively; add: pre-open the group containing the current path when the menu opens; close menu on link click, Escape, and `matchMedia('(min-width: 1001px)')` change; body scroll lock while open.
- [ ] **Step 3: Verify:** Playwright at 390×844 on `/fi/opinnot/`: menu opens, KILTA group is pre-opened (Opinnot is a child), opening ABEILLE-adjacent group closes KILTA — assert via `browser_snapshot`. Escape closes.
- [ ] **Step 4: Commit.**

### Task 10: Search restyle and behaviors

**Files:**
- Modify: `site/layouts/partials/header.html` (search markup classes), `site/assets/js/search.js`, `site/assets/css/main.css` (only if a rule proves missing)

**Interfaces:**
- Consumes: Task 7 `.search-*`, `.is-filled`, `.is-row` classes.
- Produces: desktop panel field wrapped in `.is-filled`, phone menu field in `.is-row`; a `.search-meta` row (count + `.search-clear` button) in the phone variant.

- [ ] **Step 1: Apply the class variants** to the two search blocks; placeholder text becomes "Hae sivuja" / "Search pages" (per §5); in the `.is-row` variant the placeholder is "HAKU"/"SEARCH" (CSS renders it 24px/800/caps via the existing `::placeholder` rule).
- [ ] **Step 2: In `search.js`:** autofocus the field when the desktop panel opens; clear the query on panel close; add the phone `.search-meta` row: count text on the left (existing `t.count`), a "Tyhjennä"/"Clear" button on the right that empties the input and results. Keep the combobox/arrow-key/Enter machinery and the show-all button exactly as they are.
- [ ] **Step 3: Verify with Playwright** on the dev server: open search, type "kilta", assert results render with `<mark>`, count line matches `N tulosta`, Enter navigates to the top result; phone width: Tyhjennä resets. Commit.

### Task 11: Footer

**Files:**
- Modify: `site/layouts/partials/footer.html` (rewrite)

**Interfaces:**
- Consumes: Task 5 footer.yaml shape, `data/partners.yaml`, Task 7 `.footer-*` classes.
- Produces: `.site-footer` = `.rainbow-rule` + `.container > .footer-grid` (brand column: blue logo `images/brand/logo-text.svg` + `<address>`; links column: `.footer-title` + two-column `.footer-links`; partners column: `.footer-title` "Prodeko Network" + `.footer-partners`) + `.footer-bottom` (legal left; `bottom` links right).

- [ ] **Step 1: Rewrite the partial** against that structure. Address lines from `footer.address` joined with `<br>`. No `<details>` anywhere.
- [ ] **Step 2: Verify:** build, `grep -c footer-group site/public/fi/index.html` → `0`; screenshot footer desktop + 390px. Commit.

---

## Phase 2 — page templates

### Task 12: Page frame (the v2 shell)

**Files:**
- Modify: `site/layouts/partials/page-shell.html` (rewrite)
- Modify: `site/layouts/partials/section-nav.html` (rewrite to read `sectionnav.yaml`)
- Create: `site/layouts/partials/aside-blocks.html`
- Modify: `site/layouts/partials/breadcrumbs.html` (classes only, to `.breadcrumbs` `ol` shape if it differs)

**Interfaces:**
- Consumes: Task 4 sectionnav, Task 7 classes.
- Produces: `page-shell.html` keeps its `{page, body}` dict contract (people/archive layouts depend on it). Frontmatter contract for all content tasks:

```yaml
photo: { src: images/pages/page-guild-crop.png, alt: "…", caption: "…" }  # alt required, caption optional
aside:
  - title: Kiltahuone
    text: |            # markdown; rendered with markdownify; newlines become <br>
      TUAS-talo, Maarintie 8, 02150 Espoo.
  - title: Jäsenyys
    text: "Jäsenyys maksaa 8 € lukuvuodessa."
    button: { label: Hae jäseneksi, url: "https://membership.prodeko.org/apply" }
  - title: Ajankohtaista
    links:
      - { label: "…", url: "…", description: "…" }
```

- [ ] **Step 1: Rewrite `page-shell.html`:** `.page-layout` grid (add `no-aside` when neither sectionnav match nor asides exist); main column = breadcrumbs, `.page-title` h1, optional `.figure` (photo via `resources.Get .src` — `errorf "photo %q not found on %s" …` when the key is set but the resource is nil — processed `Resize "1500x q80"`), then `.prose` with the body (keep the doc-link replaceRE). Aside column = `partial "section-nav.html"` then `partial "aside-blocks.html"`. Drop the `page-head` band and its rainbow rule.
- [ ] **Step 2: Rewrite `section-nav.html`:** find the first entry in `index hugo.Data.sectionnav .Language.Lang` (for `/sv/` pages the language is fi) whose `match` prefixes the page's `.RelPermalink`; render `.section-nav-title` + list; `aria-current="page"` on exact URL match; items with `members: true` render as a link in the members build (`site.Params.members`) and as `<span class="m-members">label (jäsenille)</span>`-style muted text publicly. No match → render nothing.
- [ ] **Step 3: Write `aside-blocks.html`:** iterate `.Params.aside`, each `.aside-block` with h2 title and `.aside-body`; `text` → `markdownify` (pre-replace `\n` with `<br>` outside list items), `button` → `a.btn.btn-primary`, `links` → `.link-list` with name/desc/arrow rows.
- [ ] **Step 4: Tests (rendered-output greps after a dev build):**
  - `/fi/guild/arvot/` contains `class="page-layout"`, a `section-nav` with `aria-current` on Arvot, no `page-head`.
  - `/fi/tietosuoja/` contains `page-layout no-aside` and **no** `page-aside` (Review Focus 1).
  - Temporarily set a bogus `photo.src` on any page → `hugo` exits non-zero with the errorf message (Review Focus 3); revert.
- [ ] **Step 5: Commit.**

### Task 13: Front page

**Files:**
- Modify: `site/layouts/home.html` (rewrite)
- Modify: `site/content/fi/_index.md`, `site/content/en/_index.md`

**Interfaces:**
- Consumes: Task 5 `news.yaml`, Task 7 `.home-hero` etc., Task 12 aside partials.
- Produces: home = `.home-hero` (default no-links variant, `--photo-guild`, h1 from `heroTitle`) + `.page-layout` (prose = page content; aside = Ajankohtaista `.aside-block` built from `news.yaml` + the page's `aside` frontmatter for Jäsenyys).

- [ ] **Step 1: Rewrite `home.html`** per §6: hero, then grid; delete stripe/emblem imgs, kicker, ctas, announcement, audiences rendering.
- [ ] **Step 2: Rewrite `_index.md` (fi, then en mirroring):** frontmatter keeps `title`, `translationKey`, `description`, `heroTitle`; delete `heroKicker`, `cta*`, `announcement`, `audiences`; add the Jäsenyys aside (fi: text "Jäsenyys maksaa 8 € lukuvuodessa.", button Hae jäseneksi → https://membership.prodeko.org/apply; en: "Membership is 8 € per academic year." / Join the guild). Body: compare against `PAGES.home.<lang>.blocks` in `../design_handoff_prodeko_site/site-data.js` and make the five paragraphs + "Yhdistämässä tutalaisia jo vuodesta 1866" H2 + "Prodekon arvot" H2 with five H3 values match it verbatim (1866 is intentional). The "täältä" links go to `/fi/guild/arvot/` and `https://membership.prodeko.org/apply`.
- [ ] **Step 3: Tests:** dev build; `/fi/` HTML contains `home-hero` without `has-links`, `1866`, three Ajankohtaista links from news.yaml, exactly one `btn-primary` in the main+aside area; screenshot vs prototype front page at 1280 and 390.
- [ ] **Step 4: Commit.**

### Task 14: People grid with photos, archive as definition lists

**Files:**
- Modify: `site/layouts/partials/people-grid.html`, `site/layouts/partials/name-rows.html` (classes)
- Modify: `site/layouts/archive.html`

**Interfaces:**
- Consumes: Task 6 `photo` field; Task 7 `.people-grid`, `.def-list`, `.archive-jump` classes; `page-shell` contract.
- Produces: `people-grid.html` renders `.person` with optional `<img>` (Hugo `.Fill "400x400 q80"`; `errorf` if `photo` set but resource missing); `archive.html` renders each year as `.archive-group` with h2 id + `.def-list` of role→name rows.

- [ ] **Step 1: `people-grid.html`:** add the photo block; keep name/role and the roleEn logic.
- [ ] **Step 2: `archive.html`:** replace the members partial call for the default display with an inline `.def-list` (`.row` per member: `dt` role — with the same roleEn fallback — `dd` name). Data order is preserved (Puheenjohtaja is first in every year file — spot-check 3 files). `peopleDisplay: rows` (honours) keeps `name-rows.html`. The jump nav renders as `.archive-jump`; anchor scroll offset comes from Task 7's `scroll-margin-top`.
- [ ] **Step 3: Tests:** `/fi/guild/hallitus-ja-toimihenkilot/hallitus/` contains 12 `<img` tags under `people-grid`; `/fi/guild/hallitus-ja-toimihenkilot/edelliset-hallitukset/` contains `archive-jump`, `id="1967"`-style anchors and `def-list` rows, zero `person` cards; honours page still renders name-rows. Screenshot board page vs restyled expectation (grid of 12 headshots, no card boxes).
- [ ] **Step 4: Commit.**

### Task 15: Retire the hub layouts (mechanism only)

**Files:**
- Modify: `site/layouts/section.html` (confirm it routes hubs through page-shell when no `layout` param)
- Delete (at end of phase 3, Task 21): `guild-hub.html`, `prospective-hub.html`, `corporate-hub.html`, `alumni-hub.html`

**Interfaces:**
- Produces: guarantee that a section `_index.md` without a `layout` param renders through the Task 12 frame. Content conversion happens per section in phase 3; the four layout files stay in place until Task 21 so unconverted hubs keep rendering mid-phase.

- [ ] **Step 1: Verify `section.html` calls `page-shell.html`** with the section content (it does today; adjust only if it bypasses the frame).
- [ ] **Step 2: Build; commit if changed.**

---

## Phase 3 — content adaptation (Workflow fan-out; one agent per task, worktree isolation not needed — tasks touch disjoint files)

Every task in this phase follows the same steps, so they are stated once: (1) edit the listed content files — frontmatter `aside`/`photo` exactly as given, `layout:` param removed where noted, body reshaped only where noted, copy otherwise untouched; (2) `hugo -s site --environment development` exits 0; (3) grep the built page for `aside-block` titles and `figure` when a photo is set; (4) commit. Aside text is markdown; addresses keep their line breaks.

### Task 16: Guild section + Swedish pages

Files: `site/content/fi/guild/_index.md` (+ en), `arvot.md`, `hairintayhdyshenkilot.md`, `jaseneksi.md`, `kunnianosoitukset.md`, `guild-rules/*` (+ en `rules/*`), `site/content/fi/abit/abiturienter/*`.
- Guild hub (fi + en): remove `layout: guild-hub`; move the `sections`/`numbers`/`docs` param content into the markdown body as h2 + paragraphs + link lists, preserving every link and sentence; photo `page-guild-crop.png` (alt: kiltalaisia TUAS-talolla); asides fi: Kiltahuone («TUAS-talo, Maarintie 8, 02150 Espoo.»), Ota yhteyttä (address 3 lines + `[hallitus@prodeko.org](mailto:hallitus@prodeko.org)`); en: Guild room / Contact mirrors.
- Other guild pages, rules pages, Swedish pages: no aside additions, no photos (the SectionNav appears automatically). Verify `/sv/` pages render the Abiturienter sectionnav and the header shows Abeille as current (Review Focus 4) — Playwright snapshot.

### Task 17: Board subsection

Files: `fi/guild/hallitus-ja-toimihenkilot/*` (+ en `board-and-officials/*`).
- `hallitus.md` / `board.md`: aside Ota yhteyttä / Contact: `[hallitus@prodeko.org](mailto:hallitus@prodeko.org)`.
- `toimihenkilot.md` / `officials.md`: same contact aside.
- Others (including edelliset/previous): no asides. Breadcrumbs must read Etusivu / Kilta / … (comes from the tree; verify only).

### Task 18: Fukseille + Opinnot + Palvelut (+ en New students / Studies / Services)

- `new-students/_index.md`: aside Fuksikapteeni: text "Lukukauden 2026–2027 fuksikapteeni on Elina Lauri." + `[fuksikapteeni@prodeko.org](mailto:fuksikapteeni@prodeko.org)`<br>`Telegram @fuksikapteeni`; photo `fuksi-group-steps.jpg` (spread decision); body: ensure a link list of the subpages exists (add as `link-list`-rendering markdown only if the body lacks navigation to them — SectionNav may make this redundant; if the body already reads fine, leave it).
- `welcome-to-prodeko.md` (fi + en): aside Fuksikapteeni with name/email/phone/Telegram per the prototype (`Elina Lauri`, `fuksikapteeni@prodeko.org`, tel `040 659 2055`, Telegram @fuksikapteeni); photo `page-prospective-crop.png` per handoff §12 (shared with Abeille until a real one exists).
- `opinnot/_index.md` / `studies/_index.md`: aside Opintovastaava / Minister of Studies: "Lauri Pörsti" + `[opintovastaava@prodeko.org](mailto:opintovastaava@prodeko.org)`.
- `palvelut/_index.md` / `services/_index.md`: aside Kulukorvaukset / Expense claims: "Killan kulut korvataan kululaskupalvelun kautta." + `[prodeko.kululaskut.fi](https://prodeko.kululaskut.fi)` (en mirror).

### Task 19: Abeille, Yrityksille, Alumni hubs

- `abit/_index.md` / `prospective-students.md` (en is a single page — check `content/en/prospective-students.md` + section dir): remove `layout: prospective-hub`; photo `page-prospective-crop.png`; aside Abivastaavat / Ask a student per the prototype copy ("Jos haluat kysyä opinnoista tai opiskelijaelämästä suoraan opiskelijalta, tavoitat abivastaavat sähköpostilla." + abivastaava@prodeko.org).
- `yrityssuhteet/_index.md` (fi + en): remove `layout: corporate-hub`; photo `page-companies.jpg`; body keeps the partner-grid shortcode/partial for Prodeko Network; aside Ota yhteyttä / Contact: "**Tuomas Ylimäki**\nYrityssuhdevastaava, hallitus 2026" + the pitch sentence + `[yrityssuhteet@prodeko.org](mailto:yrityssuhteet@prodeko.org)` + button {Ota yhteyttä / Get in touch, mailto:yrityssuhteet@prodeko.org}.
- `alumni/_index.md` (fi + en): remove `layout: alumni-hub`; photo `page-alumni.jpg`; asides Valmistuit? / Graduated? ("Olet jo mukana. Kaikki valmistuneet liitetään automaattisesti. Killan vanhaksi jäseneksi voi lisäksi hakea kuka tahansa entinen varsinainen jäsen." + alumni@prodeko.org) and Ota yhteyttä / Contact ("Prodekon Alumni ry\nPL 15500, 00076 Aalto").
- Hub bodies: move each hub layout's param-driven content (sections, stats, docs) into the markdown body, preserving all information as prose/lists — the "one primary button per page" rule means stat rows and CTA duplications are dropped only when the same link exists elsewhere on the page; anything unique is kept as text.

### Task 20: ESTIEM, strays, member pages

- `estiem-lg-helsinki/_index.md` (fi + en): photo `dock-jump.jpg` (spread decision); no aside.
- `tapahtumat.md`/`events.md`, `vaalit.md`, `matrikkeli.md`, `tietosuoja.md`/`privacy-policy.md`: verify they render in the frame (no-aside variant) and their redirect/embed shortcodes still work; no content changes.
- `site/content-members/fi/jasenille/*`, `en/members/*`: verify the frame renders, the Jäsenille sectionnav matches, embeds work in the members build.

### Task 21: Decap config + hub layout deletion

**Files:**
- Modify: `site/static/admin/config.yml`
- Delete: `site/layouts/{guild,prospective,corporate,alumni}-hub.html`

- [ ] **Step 1: Page collections (`pages_fi`, `pages_en`, both member collections):** add optional fields matching Task 12's contract — `photo` (object: image `src`, string `alt`, optional string `caption`) and `aside` (list of objects: string `title`, optional text `text`, optional object `button` {label, url}, optional list `links` {label, url, description}).
- [ ] **Step 2: `special_pages`:** replace the eight `*_hub_*` file entries' field lists with the standard page fields (title, body, photo, aside); update the two `home_*` entries to the Task 13 frontmatter (drop announcement/audiences/cta fields, add aside).
- [ ] **Step 3: Add a `news` file collection** editing `site/data/news.yaml` (per-language title + items list), and add the `photo` string field to the boards-2026 collection if boards are Decap-editable (check; if boards are not in Decap, skip and note it).
- [ ] **Step 4: Validate:** `npx --yes decap-server & ` is not needed — Decap config is static YAML; validate shape with `python3 -c "import yaml; yaml.safe_load(open('site/static/admin/config.yml'))"` and by loading `/admin/` on the dev server (config parse errors render in-page).
- [ ] **Step 5: Delete the four hub layouts; full build both envs must pass (proves no `layout:` param references them). Commit.**

---

## Phase 4 — verification (Workflow, loop until dry)

### Task 22: Automated sweep

- [ ] **Step 1: Build everything:** `hugo -s site --minify --cleanDestinationDir` ; `hugo -s site --minify --cleanDestinationDir --environment members` ; dev build for preview. Run Pagefind the way `.github/workflows/build.yml` does if the local dev flow needs it for search tests.
- [ ] **Step 2: Audit both trees:** serve `site/public-preview`, run `node tools/audit-site.mjs http://localhost:1313 site/public-preview` → zero broken links, zero orphans (Review Focus 5), zero 390px overflows. Repeat against the members tree.
- [ ] **Step 3: URL preservation:** `git ls-tree -r --name-only main -- site/content site/content-members | sort` equals the same listing on this branch (no file renames); additionally diff the built page inventory: list of `index.html` paths under `public` on main's build vs this branch's — only additions allowed.

### Task 23: Visual pass against the prototype

- [ ] **Step 1: Serve the prototype:** `devctl start proto -- python3 -m http.server 8100 -d ../design_handoff_prodeko_site` and the site dev server; screenshot each mocked page (Etusivu, Kilta, Hallitus 2026, Edelliset hallitukset, Abeille, Fukseille, Tervetuloa tutalle!, Opinnot, Palvelut, Yrityksille, Alumni) at 1280 and 390 in both, side by side.
- [ ] **Step 2: Interaction checklist** per handoff §10 with Playwright: mega opens on hover and closes on outside click/Escape; search per Task 10's behaviors; phone menu per Task 9's; lang switch lands on the counterpart URL; external links get `target="_blank"` (render-link hook — verify it still applies).
- [ ] **Step 3: Fix loop.** Every mismatch becomes a fix commit; re-run the failed check; loop until a full pass is dry. Off-mockup pages get a lighter sweep: screenshot one page per section at both widths and check nothing violates the design law (cards, radii, shadows, second buttons).

---

## Execution note (method chosen by Risto: dynamic workflow)

- Phases 0–2: run as a sequence — each task one `agent()` call (opus), gated on the previous (chrome tasks share `main.css`/`header.html`).
- Phase 3: `pipeline(tasks 16–20)` in parallel (disjoint files) with Task 21 after the barrier.
- Phase 4: loop-until-dry — sweep agents produce findings, fix agents consume them, re-sweep; two consecutive clean rounds end the run.
- Phases run end-to-end without pauses; report at completion or on a genuine blocker.
