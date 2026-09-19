// The search box in the header, driving Pagefind's JavaScript API directly.
// Pagefind's own user interface bundle is not used: a result here is a title
// and a two-line excerpt in the site's own type, inside a panel built out of
// the same parts as a mega panel.
//
// Same idiom as mega-menu.js: an IIFE, var, no modules, so the two files can be
// concatenated into one script tag.
(function () {
  var boxes = Array.prototype.slice.call(document.querySelectorAll(".search-box"));
  if (!boxes.length) return;

  var header = document.getElementById("site-header");
  var fi = document.documentElement.lang === "fi";
  var t = {
    loading: fi ? "Haetaan…" : "Searching…",
    failed: fi ? "Haku ei ole käytettävissä." : "Search is unavailable.",
    // An expired session, not a broken site. Public results keep working, so
    // the line says what is missing rather than that search is down.
    gated: fi
      ? "Jäsensisältö ei ole nyt haettavissa — kirjaudu uudelleen."
      : "Member content is not searchable right now — sign in again.",
    empty: function (q) {
      return (fi ? 'Ei tuloksia haulla "' : 'No results for "') + q + '"';
    },
    count: function (n) {
      if (fi) return n + (n === 1 ? " tulos" : " tulosta");
      return n + (n === 1 ? " result" : " results");
    },
    all: function (n) {
      return fi ? "Näytä kaikki " + n + " tulosta" : "Show all " + n + " results";
    }
  };

  // Imported on first open rather than on page load, so a reader who never
  // searches pays nothing. One instance for every box on the page, and one
  // member merge, cached as promises.
  var loading = null;
  var merging = null;
  var memberFailed = false;

  function pagefind() {
    if (!loading) loading = import("/pagefind/pagefind.js");
    return loading;
  }

  // Probed before it is merged, and never merged speculatively. A failed
  // mergeIndex poisons the instance: every later search() on it throws too, so
  // a lapsed session would get a dead search box rather than public results.
  // The probe checks the content type because Keycloak answers an expired
  // session with a 200 and an HTML login page, which passes an ok check.
  async function mergeMemberIndex(pf, bundle) {
    try {
      var r = await fetch(bundle + "/pagefind-entry.json", { credentials: "same-origin" });
      var ct = r.headers.get("content-type") || "";
      if (!r.ok || ct.indexOf("json") === -1) return false;
      JSON.parse(await r.text());
      // baseUrl is required: without it Pagefind treats the merged index as a
      // separate site and prefixes every member result with the bundle's path.
      await pf.mergeIndex(bundle, { baseUrl: "/" });
      return true;
    } catch (e) {
      return false;
    }
  }

  function ready(bundle) {
    return pagefind().then(function (pf) {
      if (!bundle) return pf;
      if (!merging) {
        merging = mergeMemberIndex(pf, bundle).then(function (ok) {
          memberFailed = !ok;
          return ok;
        });
      }
      return merging.then(function () { return pf; });
    });
  }

  // Where a result should land: on the matched text, not the page top. Two
  // layers, because only one of them works everywhere. Pagefind's sub-results
  // carry the nearest heading's id, which Hugo generates for every heading, so
  // the anchor is an ordinary address any browser honours. The text fragment
  // appended after it is honoured by the browsers that have it and ignored by
  // the rest, which then fall back to the anchor.
  function resultHref(d) {
    var best = null;
    var top = -1;
    (d.sub_results || []).forEach(function (s) {
      var score = (s.weighted_locations || []).reduce(function (a, w) { return a + w.balanced_score; }, 0);
      if (score > top) { top = score; best = s; }
    });
    if (!best) best = { url: d.url, excerpt: d.excerpt };
    // The excerpt is cut from the page's own text, so a phrase taken out of it
    // is there to be found. innerHTML and a range rather than a regex: it
    // decodes the entities and drops the <mark> tags in one step.
    var host = document.createElement("span");
    host.innerHTML = best.excerpt;
    var mark = host.querySelector("mark");
    if (!mark) return best.url;
    var range = document.createRange();
    range.setStartBefore(mark);
    range.setEnd(host, host.childNodes.length);
    var words = [];
    range.toString().trim().split(/\s+/).slice(0, 3).some(function (w) {
      // Pagefind ends every block with a full stop the page itself may not
      // contain, so the phrase stops at one rather than quoting text that is
      // not there.
      var ends = /[.!?]$/.test(w);
      words.push(ends ? w.slice(0, -1) : w);
      return ends;
    });
    // One word on its own is matched wherever it first appears, which can be
    // above the section the anchor names. The anchor alone is better than that.
    if (words.length < 2) return best.url;
    return best.url + (best.url.indexOf("#") === -1 ? "#" : "") +
      ":~:text=" + encodeURIComponent(words.join(" "));
  }

  // The panel. Only the desktop box has one; the phone form sits inside the
  // menu that is already open around it.
  var toggle = document.querySelector("[data-search-toggle]");
  var panel = document.getElementById("search-panel");

  function openPanel() {
    // mega-menu.js already closes every mega panel on the header's mouseleave,
    // so opening search can reuse that rather than reach into it.
    if (header) header.dispatchEvent(new Event("mouseleave"));
    panel.hidden = false;
    toggle.setAttribute("aria-expanded", "true");
    panel.querySelector("[data-search]").focus();
  }

  function closePanel(keepFocus) {
    panel.hidden = true;
    toggle.setAttribute("aria-expanded", "false");
    if (!keepFocus) toggle.focus();
  }

  if (toggle && panel) {
    toggle.addEventListener("click", function () {
      if (panel.hidden) openPanel(); else closePanel();
    });

    document.addEventListener("click", function (e) {
      if (!panel.hidden && header && !header.contains(e.target)) closePanel(true);
    });
  }

  boxes.forEach(function (box) {
    var input = box.querySelector("[data-search]");
    var list = box.querySelector(".search-results");
    var footer = box.querySelector(".search-footer");
    var status = box.querySelector(".search-status");
    var bundle = input.getAttribute("data-search-members");
    var rows = [];
    var results = [];
    var active = -1;
    var limit = 6;
    var query = "";
    var timer = null;
    var announce = null;

    // The count chatters through a word if every repaint announces itself, so
    // it waits 300ms; anything else is a one-off and is said at once.
    function say(text, soon) {
      clearTimeout(announce);
      if (soon) { status.textContent = text; return; }
      announce = setTimeout(function () { status.textContent = text; }, 300);
    }

    function highlight(i) {
      if (rows[active]) rows[active].classList.remove("is-active");
      active = i;
      if (rows[active]) {
        rows[active].classList.add("is-active");
        input.setAttribute("aria-activedescendant", rows[active].id);
        rows[active].scrollIntoView({ block: "nearest" });
      } else {
        input.removeAttribute("aria-activedescendant");
      }
    }

    function clear() {
      list.textContent = "";
      footer.textContent = "";
      list.classList.remove("is-expanded");
      rows = [];
      highlight(-1);
      input.setAttribute("aria-expanded", "false");
      say("", true);
    }

    function paint() {
      var painting = query;
      var shown = results.slice(0, limit);
      // data() is one fragment fetch each, so only the rendered rows ask for
      // one. This is what bounds what a search costs on the wire.
      return Promise.all(shown.map(function (r) { return r.data(); })).then(function (data) {
        // The reader typed on while these fragments were in flight, and the
        // newer query has results of its own. Writing these now would put the
        // old query's rows under the new query's text.
        if (painting !== query) return;
        list.textContent = "";
        footer.textContent = "";
        rows = data.map(function (d, i) {
          var a = document.createElement("a");
          a.className = "search-result";
          a.setAttribute("role", "option");
          a.tabIndex = -1;
          a.id = list.id + "-option-" + i;
          a.href = resultHref(d);
          var title = document.createElement("span");
          title.className = "search-result-title";
          title.textContent = (d.meta && d.meta.title) || d.url;
          var excerpt = document.createElement("span");
          excerpt.className = "search-result-excerpt";
          // Pagefind's own HTML, with <mark> already around the matched words.
          // The title and the address are ours and are not parsed as HTML.
          excerpt.innerHTML = d.excerpt;
          a.appendChild(title);
          a.appendChild(excerpt);
          list.appendChild(a);
          return a;
        });
        if (results.length > rows.length) {
          var more = document.createElement("button");
          more.type = "button";
          more.className = "search-more";
          more.textContent = t.all(results.length);
          more.addEventListener("click", function () {
            limit = results.length;
            list.classList.add("is-expanded");
            paint().then(function () { input.focus(); });
          });
          footer.appendChild(more);
        }
        input.setAttribute("aria-expanded", rows.length ? "true" : "false");
        highlight(rows.length ? 0 : -1);
        var line = rows.length ? t.count(results.length) : t.empty(query);
        say(memberFailed ? line + " · " + t.gated : line);
      });
    }

    function run() {
      var q = query;
      if (!q) { clear(); return; }
      ready(bundle)
        .then(function (pf) { return pf.search(q); })
        .then(function (res) {
          // The reader has typed on; that query's results are on their way.
          if (q !== query) return;
          results = res.results;
          limit = 6;
          list.classList.remove("is-expanded");
          return paint();
        })
        .catch(function () {
          clear();
          say(t.failed, true);
        });
    }

    // Opening the panel focuses the input, which is where the import starts:
    // by the time the first keystroke lands the module is usually there. The
    // rejection is swallowed here and reported by the first search instead.
    input.addEventListener("focus", function () { pagefind().catch(function () {}); });

    // Pagefind fetches over the network rather than searching in memory, so an
    // undebounced keystroke costs requests. 120ms collapses a fast typist's
    // keystrokes and still repaints inside the interval a reader reads as
    // immediate.
    input.addEventListener("input", function () {
      query = input.value.trim();
      clearTimeout(timer);
      if (!query) { clear(); return; }
      if (!rows.length) say(t.loading, true);
      timer = setTimeout(run, 120);
    });

    input.addEventListener("keydown", function (e) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        if (!rows.length) return;
        e.preventDefault();
        var next = active + (e.key === "ArrowDown" ? 1 : -1);
        highlight((next + rows.length) % rows.length);
      } else if (e.key === "Enter") {
        // Type and press Enter is the common case, so Enter with nothing
        // highlighted goes to the first result.
        var row = rows[active] || rows[0];
        if (row) { e.preventDefault(); window.location.href = row.href; }
      } else if (e.key === "Escape") {
        // Stops here, or the document-level Escape handler in mega-menu.js
        // fires as well.
        if (panel && panel.contains(input)) { e.stopPropagation(); closePanel(); }
      }
    });
  });
})();
