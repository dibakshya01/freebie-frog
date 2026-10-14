/* freebie-frog — shared behaviour: theme toggle, copy buttons, mobile nav,
   gated scroll-reveal. Vanilla, no dependencies. All motion is gated behind
   prefers-reduced-motion; the page is complete and readable without this file. */
(function () {
  "use strict";

  var reduceMotion = false;
  try {
    reduceMotion = window.matchMedia &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  } catch (e) { reduceMotion = false; }

  /* ---- Theme toggle (persisted per-viewer) -------------------------------- */
  function storedTheme() {
    try { return localStorage.getItem("ff-theme"); } catch (e) { return null; }
  }
  function storeTheme(v) {
    try { if (v) localStorage.setItem("ff-theme", v); else localStorage.removeItem("ff-theme"); }
    catch (e) { /* private mode — ignore */ }
  }
  function systemDark() {
    try { return window.matchMedia("(prefers-color-scheme: dark)").matches; }
    catch (e) { return false; }
  }
  function applyTheme(v) {
    var root = document.documentElement;
    if (v === "dark" || v === "light") root.setAttribute("data-theme", v);
    else root.removeAttribute("data-theme");
  }

  // Early inline snippet already set the attribute to avoid flash; sync here too.
  applyTheme(storedTheme());

  function wireThemeToggle() {
    var btn = document.querySelector(".theme-toggle");
    if (!btn) return;
    btn.addEventListener("click", function () {
      var current = document.documentElement.getAttribute("data-theme");
      var isDark = current ? current === "dark" : systemDark();
      var next = isDark ? "light" : "dark";
      applyTheme(next);
      storeTheme(next);
      btn.setAttribute("aria-label", "Switch to " + (next === "dark" ? "light" : "dark") + " theme");
    });
  }

  /* ---- Copy buttons ------------------------------------------------------- */
  function flash(btn) {
    btn.classList.add("copied");
    btn.setAttribute("aria-label", "Copied");
    window.setTimeout(function () {
      btn.classList.remove("copied");
      btn.setAttribute("aria-label", "Copy to clipboard");
    }, 1600);
  }
  function legacyCopy(text) {
    try {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "absolute";
      ta.style.left = "-9999px";
      document.body.appendChild(ta);
      ta.select();
      document.execCommand("copy");
      document.body.removeChild(ta);
      return true;
    } catch (e) { return false; }
  }
  function wireCopy() {
    var btns = document.querySelectorAll("[data-copy]");
    Array.prototype.forEach.call(btns, function (btn) {
      btn.addEventListener("click", function () {
        var sel = btn.getAttribute("data-copy");
        var src = sel ? document.querySelector(sel) : btn.previousElementSibling;
        var text = btn.getAttribute("data-copy-text") || (src ? src.textContent : "");
        text = (text || "").trim();
        if (!text) return;
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(text).then(function () { flash(btn); },
            function () { if (legacyCopy(text)) flash(btn); });
        } else {
          if (legacyCopy(text)) flash(btn);
        }
      });
    });
  }

  /* ---- Mobile nav --------------------------------------------------------- */
  function wireNav() {
    var burger = document.querySelector(".nav__burger");
    var links = document.getElementById("nav-links");
    if (!burger || !links) return;
    burger.addEventListener("click", function () {
      var open = !links.hidden;
      links.hidden = open;
      burger.setAttribute("aria-expanded", String(!open));
    });
    // Close the menu when a link is chosen or on resize to desktop.
    links.addEventListener("click", function (e) {
      if (e.target.tagName === "A" && window.innerWidth <= 820) {
        links.hidden = true;
        burger.setAttribute("aria-expanded", "false");
      }
    });
    function sync() {
      if (window.innerWidth > 820) { links.hidden = false; }
      else if (burger.getAttribute("aria-expanded") !== "true") { links.hidden = true; }
    }
    window.addEventListener("resize", sync);
    sync();
  }

  /* ---- Scroll reveal (gated) ---------------------------------------------- */
  function wireReveal() {
    var items = document.querySelectorAll(".reveal");
    if (!items.length) return;

    // If motion is off or the API is missing, leave everything visible.
    if (reduceMotion || !("IntersectionObserver" in window)) return;

    document.documentElement.classList.add("has-motion");

    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (en.isIntersecting) {
          en.target.classList.add("is-in");
          io.unobserve(en.target);
        }
      });
    }, { rootMargin: "0px 0px -8% 0px", threshold: 0.08 });

    Array.prototype.forEach.call(items, function (el) { io.observe(el); });

    // Safety net: reveal everything after 2.5s in case the observer never fires.
    window.setTimeout(function () {
      Array.prototype.forEach.call(items, function (el) { el.classList.add("is-in"); });
    }, 2500);
  }

  function init() {
    wireThemeToggle();
    wireCopy();
    wireNav();
    wireReveal();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
