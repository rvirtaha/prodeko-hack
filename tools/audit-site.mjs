#!/usr/bin/env node
/**
 * Crawls a built copy of the site and reports what is wrong with it.
 *
 * Four things go wrong on a site assembled out of a scrape, and none of them
 * show up in a Hugo build that exits zero:
 *
 *   - a page is built but nothing links to it, so no reader will ever see it
 *   - a link points at a page that no longer exists, here or elsewhere
 *   - a page is wider than a phone, so it scrolls sideways
 *   - an asset or an embed fails, leaving a hole the templates cannot see
 *
 * Usage:
 *
 *   hugo -s site --environment development
 *   npx serve site/public-preview -l 1313     # or any static server
 *   node tools/audit-site.mjs http://localhost:1313 site/public-preview
 *
 * Requires playwright. Exits non-zero if it finds a broken internal link, an
 * orphaned page or a page that overflows a 390px viewport.
 */
import { chromium } from 'playwright';
import fs from 'node:fs';
import path from 'node:path';

const BASE = (process.argv[2] || 'http://localhost:1313').replace(/\/$/, '');
const BUILD_DIR = process.argv[3] || 'site/public-preview';
const JSON_OUT = process.argv[4] || null;

const MOBILE = { width: 390, height: 844 };
const DESKTOP = { width: 1440, height: 900 };

/** Prodeko's own services are not resolvable from a build machine, and a
 *  handful of large sites answer a headless browser with 403 while serving a
 *  real one fine. Neither is a broken link, so neither is reported. */
const UNREACHABLE_HOSTS = [
  'viikkotiedote.prodeko.org',
  'store.prodeko.org',
  'gallery.prodeko.org',
  'ilmo.prodeko.org',
  'membership.prodeko.org',
  'alumni.prodeko.org',
  'vaalit.prodeko.org',
  'vaalikoppi.prodeko.org',
  'prodeko.kululaskut.fi',
];
const BOT_HOSTILE = /(^|\.)(aalto\.fi|hsl\.fi|nokia\.com|abb\.com|ayy\.fi|reittiopas\.fi|linkedin\.com|instagram\.com|facebook\.com)$/;

/** The member sections live in a tree of their own, served behind the login
 *  gate and absent from the tree crawled here, so the sign-in button in the
 *  header points at an address this crawl cannot resolve. It is the door to
 *  the gate rather than a broken link: skip it, which keeps it out of the
 *  queue as well as out of the report. */
const MEMBER_SECTIONS = /^\/(fi\/jasenille|en\/members)(\/|$)/;

const origin = new URL(BASE).origin;
const norm = (u) => { const x = new URL(u, BASE); x.hash = ''; return x.href; };
const pathOf = (u) => new URL(u).pathname;

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: DESKTOP, ignoreHTTPSErrors: true });
const page = await ctx.newPage();

const pages = new Map();
const external = new Map();
const queue = [norm(`${BASE}/fi/`), norm(`${BASE}/en/`)];
const seen = new Set(queue);

const consoleErrors = [];
const failedRequests = [];
page.on('console', (m) => m.type() === 'error' && consoleErrors.push(m.text()));
page.on('requestfailed', (r) => failedRequests.push(`${r.url()} :: ${r.failure()?.errorText}`));
page.on('response', (r) => r.status() >= 400 && failedRequests.push(`${r.url()} :: HTTP ${r.status()}`));

while (queue.length) {
  const url = queue.shift();
  consoleErrors.length = 0;
  failedRequests.length = 0;
  const record = { url };

  let response;
  try {
    response = await page.goto(url, { waitUntil: 'networkidle', timeout: 30000 });
  } catch (e) {
    record.error = String(e).split('\n')[0];
    pages.set(url, record);
    continue;
  }
  record.status = response?.status() ?? 0;

  const info = await page.evaluate(() => {
    const abs = (h) => { try { return new URL(h, location.href).href; } catch { return null; } };
    return {
      title: document.title,
      headings: [...document.querySelectorAll('h1')].map((h) => h.textContent.trim()),
      textLength: (document.body.innerText || '').trim().length,
      links: [...document.querySelectorAll('a[href]')].map((a) => ({
        href: a.getAttribute('href'),
        abs: abs(a.getAttribute('href')),
        text: (a.textContent || '').trim().slice(0, 60),
      })),
      images: [...document.querySelectorAll('img[src]')].map((i) => abs(i.getAttribute('src'))),
    };
  });
  Object.assign(record, info);

  await page.setViewportSize(MOBILE);
  await page.waitForTimeout(200);
  record.mobile = await page.evaluate(() => {
    const de = document.documentElement;
    const vw = de.clientWidth;
    const offenders = [];
    for (const el of document.querySelectorAll('body *')) {
      const box = el.getBoundingClientRect();
      if (box.width === 0 && box.height === 0) continue;
      const style = getComputedStyle(el);
      if (style.position === 'fixed') continue;
      // A visually-hidden element is clipped, so its box is not real overflow.
      if (style.clipPath && style.clipPath !== 'none') continue;
      if (box.right > vw + 1) {
        offenders.push({
          tag: el.tagName.toLowerCase(),
          cls: String(el.className || '').slice(0, 60),
          right: Math.round(box.right),
          text: (el.textContent || '').trim().slice(0, 40),
        });
      }
    }
    return { scrollWidth: de.scrollWidth, viewport: vw, offenders: offenders.slice(0, 6), offenderCount: offenders.length };
  });
  await page.setViewportSize(DESKTOP);

  record.consoleErrors = [...new Set(consoleErrors)];
  record.failedRequests = [...new Set(failedRequests)];
  record.internalLinks = [];

  for (const link of info.links) {
    if (!link.abs) continue;
    if (link.abs.startsWith(origin) && MEMBER_SECTIONS.test(new URL(link.abs).pathname)) continue;
    if (link.abs.startsWith(origin)) {
      const target = norm(link.abs);
      record.internalLinks.push({ ...link, target });
      if (!seen.has(target)) { seen.add(target); queue.push(target); }
    } else if (/^https?:/.test(link.abs)) {
      const host = new URL(link.abs).hostname;
      if (UNREACHABLE_HOSTS.includes(host)) continue;
      if (!external.has(link.abs)) external.set(link.abs, new Set());
      external.get(link.abs).add(url);
    }
  }
  pages.set(url, record);
}

/** Check a batch of URLs for a status, keeping concurrency modest. */
async function statuses(urls, size = 8) {
  const out = [];
  for (let i = 0; i < urls.length; i += size) {
    out.push(...await Promise.all(urls.slice(i, i + size).map(async (u) => {
      try {
        const r = await ctx.request.get(u, { timeout: 20000, maxRedirects: 5 });
        return { url: u, status: r.status() };
      } catch (e) {
        return { url: u, status: 0, error: String(e).split('\n')[0] };
      }
    })));
  }
  return out;
}

const externalResults = (await statuses([...external.keys()]))
  .map((r) => ({ ...r, referrers: [...external.get(r.url)].map(pathOf) }));

const imageUrls = [...new Set([...pages.values()].flatMap((p) => p.images || []).filter(Boolean))];
const imageResults = await statuses(imageUrls, 10);

await browser.close();

// ---- report ----------------------------------------------------------------

const crawled = new Set([...pages.keys()].map(pathOf));
/** An alias is a generated meta-refresh stub standing in for a retired
 *  address. Nothing links to one by design, so it is not an orphan. */
const isAlias = (file) => /<meta[^>]+http-equiv=["']?refresh/i.test(fs.readFileSync(file, 'utf8').slice(0, 2000));

const built = fs.existsSync(BUILD_DIR)
  ? walk(BUILD_DIR).filter((f) => f.endsWith('index.html') && !isAlias(f))
      .map((f) => '/' + path.relative(BUILD_DIR, f).replace(/index\.html$/, '').replace(/\\/g, '/'))
      .filter((u) => u !== '/')
  : [];

function walk(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? walk(path.join(dir, e.name)) : [path.join(dir, e.name)]);
}

const brokenInternal = [];
for (const p of pages.values()) {
  for (const link of p.internalLinks || []) {
    const target = pages.get(link.target);
    if (!target || target.error || target.status >= 400) {
      brokenInternal.push({ from: pathOf(p.url), to: pathOf(link.target), text: link.text });
    }
  }
}

const orphans = built.filter((b) => !crawled.has(b)).sort();
/** /admin/ is Decap's own interface, which is not responsive and is not ours
 *  to lay out. Editors use it on a laptop. */
const overflowing = [...pages.values()]
  .filter((p) => !pathOf(p.url).startsWith('/admin'))
  .filter((p) => p.mobile?.scrollWidth > p.mobile?.viewport + 1);
const badExternal = externalResults.filter((r) => (r.status >= 400 || r.status === 0) && !BOT_HOSTILE.test(new URL(r.url).hostname));
const badImages = imageResults.filter((r) => r.status >= 400 || r.status === 0);

const lines = [];
const section = (title, items, render) => {
  lines.push(`\n## ${title} (${items.length})`);
  items.forEach((i) => lines.push('- ' + render(i)));
};

lines.push(`# Site audit — ${pages.size} pages crawled of ${built.length} built`);
section('Broken internal links', brokenInternal, (b) => `${b.from} -> ${b.to}  [${b.text}]`);
section('Orphaned pages, built but linked from nowhere', orphans, (o) => o);
section('Pages wider than a 390px viewport', overflowing, (p) =>
  `${pathOf(p.url)}  ${p.mobile.scrollWidth}px` +
  p.mobile.offenders.map((o) => `\n    ${o.tag}.${o.cls} right=${o.right} "${o.text}"`).join(''));
section('Broken images', badImages, (i) => `${i.status} ${i.url}`);
section('External links failing', badExternal, (e) => `${e.status} ${e.url}\n    from: ${e.referrers.join(', ')}`);

const errorIndex = new Map();
for (const p of pages.values()) {
  for (const e of [...(p.consoleErrors || []), ...(p.failedRequests || [])]) {
    const key = e.replace(/^https?:\/\/[^/]+/, '');
    if (!errorIndex.has(key)) errorIndex.set(key, []);
    errorIndex.get(key).push(pathOf(p.url));
  }
}
section('Console errors and failed requests', [...errorIndex], ([e, where]) =>
  `${e}  (${where.length} pages, e.g. ${where.slice(0, 3).join(', ')})`);

const report = lines.join('\n');
console.log(report);
if (JSON_OUT) {
  fs.writeFileSync(JSON_OUT, JSON.stringify({ pages: [...pages.values()], external: externalResults, images: imageResults }, null, 1));
}

const failures = brokenInternal.length + orphans.length + overflowing.length + badImages.length;
process.exit(failures ? 1 : 0);
