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
    if (e.key === "Escape") closeAll();
  });

  document.addEventListener("click", function (e) {
    if (!header.contains(e.target)) closeAll();
  });

  // The footer's four link groups are <details open>, so the whole footer is
  // there for a reader without CSS or without JavaScript. Below 720px that is
  // about 1500px of link under every page, so close them; the media query in
  // main.css gives them a summary row to reopen from.
  var footerGroups = document.querySelectorAll(".footer-group");
  var phone = window.matchMedia("(max-width: 720px)");
  function syncFooter(e) {
    if (!e.matches) return;
    footerGroups.forEach(function (g) { g.open = false; });
  }
  syncFooter(phone);
  phone.addEventListener("change", syncFooter);

  // Mobile nav toggle: below 1100px (see main.css) the nav list and the
  // language/login block are hidden behind this button instead of shown
  // inline, since there's no room for them next to the logo.
  var toggle = header.querySelector(".nav-toggle");
  if (toggle) {
    toggle.addEventListener("click", function () {
      var isOpen = header.classList.toggle("nav-open");
      toggle.setAttribute("aria-expanded", isOpen ? "true" : "false");
    });
  }
})();
