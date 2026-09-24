(function () {
  var header = document.getElementById("site-header");
  if (!header) return;

  var items = header.querySelectorAll("[data-mega]");
  var panels = header.querySelectorAll("[data-mega-panel]");

  function closeAll() {
    panels.forEach(function (p) { p.classList.remove("is-open"); });
    items.forEach(function (i) { i.classList.remove("is-open"); });
  }

  function open(key) {
    closeAll();
    var item = header.querySelector('[data-mega="' + key + '"]');
    var panel = header.querySelector('[data-mega-panel="' + key + '"]');
    if (item) item.classList.add("is-open");
    if (panel) panel.classList.add("is-open");
  }

  items.forEach(function (item) {
    var key = item.getAttribute("data-mega");
    item.addEventListener("mouseenter", function () { open(key); });
    item.addEventListener("focusin", function () { open(key); });
  });

  header.addEventListener("mouseleave", closeAll);

  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") {
      closeAll();
      setMenu(false);
    }
  });

  document.addEventListener("click", function (e) {
    if (!header.contains(e.target)) closeAll();
  });

  // Phone menu: below 1000px (see main.css) the nav list and the language and
  // member actions are hidden behind this button instead of shown inline,
  // since there's no room for them next to the emblem.
  var toggle = header.querySelector(".nav-toggle");
  var mobileNav = document.getElementById("mobile-nav");
  var desktop = window.matchMedia("(min-width: 1001px)");

  // The group holding the page the reader is on, opened as the menu appears so
  // the neighbours of that page are the first thing under the thumb. The match
  // is on address prefix rather than on aria-current, which only marks a hub
  // itself: a reader deep inside Opinnot belongs to the same group as its hub.
  function openCurrentGroup() {
    var path = window.location.pathname;
    var groups = mobileNav.querySelectorAll(".m-group");
    var current = null;
    var longest = 0;
    groups.forEach(function (group) {
      group.querySelectorAll("a[href]").forEach(function (link) {
        var href = link.getAttribute("href");
        if (path.indexOf(href) === 0 && href.length > longest) {
          current = group;
          longest = href.length;
        }
      });
    });
    groups.forEach(function (group) { group.open = group === current; });
  }

  function setMenu(open) {
    if (!mobileNav) return;
    header.classList.toggle("nav-open", open);
    if (toggle) toggle.setAttribute("aria-expanded", open ? "true" : "false");
    // The menu is a fixed panel with its own scroll; leaving the page behind it
    // scrollable lets a thumb drag the article instead of the menu.
    document.body.style.overflow = open ? "hidden" : "";
    if (open) openCurrentGroup();
  }

  if (toggle) {
    toggle.addEventListener("click", function () {
      setMenu(!header.classList.contains("nav-open"));
    });
  }

  if (mobileNav) {
    mobileNav.addEventListener("click", function (e) {
      if (e.target.closest("a[href]")) setMenu(false);
    });
  }

  // A rotated phone crosses the breakpoint and the bar comes back; a menu left
  // open would then cover the page with a panel nothing can close.
  desktop.addEventListener("change", function (e) {
    if (e.matches) setMenu(false);
  });
})();
