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

  /* ---- Hero ambient canvas: "the pond" (gated) ---------------------------- */
  // A calm network of drifting nodes — green on the ingress (left) side, cyan on
  // the egress (right) side — with faint links and the occasional packet hopping
  // between neighbours. Purely decorative; the hero is complete without it.
  function wirePond() {
    var cv = document.getElementById("pond");
    if (!cv || reduceMotion) return;
    var ctx;
    try { ctx = cv.getContext("2d"); } catch (e) { return; }
    if (!ctx) return;

    var GREEN = "#35e089", CYAN = "#57bada";
    var W = 0, H = 0, dpr = Math.min(window.devicePixelRatio || 1, 2);
    var nodes = [], pulses = [], raf = null;
    var LINK = 150;

    function build() {
      var count = Math.max(10, Math.min(28, Math.round(W / 64)));
      nodes = [];
      for (var i = 0; i < count; i++) {
        nodes.push({
          x: Math.random() * W, y: Math.random() * H,
          vx: (Math.random() - 0.5) * 0.17, vy: (Math.random() - 0.5) * 0.17,
          r: 1.3 + Math.random() * 1.7
        });
      }
      pulses = [];
    }

    function resize() {
      var r = cv.getBoundingClientRect();
      W = r.width; H = r.height;
      if (W < 2 || H < 2) return;
      cv.width = Math.round(W * dpr); cv.height = Math.round(H * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      build();
    }

    function spawnPulse() {
      if (nodes.length < 2) return;
      var a = (Math.random() * nodes.length) | 0;
      var best = -1, bestD = Infinity;
      for (var j = 0; j < nodes.length; j++) {
        if (j === a) continue;
        var dx = nodes[j].x - nodes[a].x, dy = nodes[j].y - nodes[a].y;
        var d = dx * dx + dy * dy;
        if (d > 900 && d < bestD) { bestD = d; best = j; }
      }
      if (best < 0) return;
      pulses.push({ a: a, b: best, t: 0, sp: 0.007 + Math.random() * 0.009, green: nodes[a].x < W * 0.5 });
    }

    function frame() {
      raf = requestAnimationFrame(frame);
      if (W < 2 || H < 2) return;
      ctx.clearRect(0, 0, W, H);

      var i, j, n;
      for (i = 0; i < nodes.length; i++) {
        n = nodes[i];
        n.x += n.vx; n.y += n.vy;
        if (n.x <= 0 || n.x >= W) n.vx *= -1;
        if (n.y <= 0 || n.y >= H) n.vy *= -1;
      }
      // links
      ctx.lineWidth = 1;
      for (i = 0; i < nodes.length; i++) {
        for (j = i + 1; j < nodes.length; j++) {
          var ddx = nodes[i].x - nodes[j].x, ddy = nodes[i].y - nodes[j].y;
          var dist = Math.sqrt(ddx * ddx + ddy * ddy);
          if (dist < LINK) {
            ctx.strokeStyle = "rgba(170,192,180," + ((1 - dist / LINK) * 0.11).toFixed(3) + ")";
            ctx.beginPath(); ctx.moveTo(nodes[i].x, nodes[i].y); ctx.lineTo(nodes[j].x, nodes[j].y); ctx.stroke();
          }
        }
      }
      // nodes
      for (i = 0; i < nodes.length; i++) {
        n = nodes[i];
        ctx.globalAlpha = 0.5;
        ctx.fillStyle = n.x < W * 0.5 ? GREEN : CYAN;
        ctx.beginPath(); ctx.arc(n.x, n.y, n.r, 0, 6.2832); ctx.fill();
      }
      ctx.globalAlpha = 1;
      // pulses
      for (var k = pulses.length - 1; k >= 0; k--) {
        var p = pulses[k]; p.t += p.sp;
        if (p.t >= 1 || !nodes[p.a] || !nodes[p.b]) { pulses.splice(k, 1); continue; }
        var x = nodes[p.a].x + (nodes[p.b].x - nodes[p.a].x) * p.t;
        var y = nodes[p.a].y + (nodes[p.b].y - nodes[p.a].y) * p.t;
        ctx.fillStyle = p.green ? GREEN : CYAN;
        ctx.globalAlpha = 0.85 * Math.sin(p.t * Math.PI);
        ctx.beginPath(); ctx.arc(x, y, 2.4, 0, 6.2832); ctx.fill();
      }
      ctx.globalAlpha = 1;
      if (pulses.length < 5 && Math.random() < 0.03) spawnPulse();
    }

    window.addEventListener("resize", resize, { passive: true });
    document.addEventListener("visibilitychange", function () {
      if (document.hidden) { if (raf) { cancelAnimationFrame(raf); raf = null; } }
      else if (!raf) { raf = requestAnimationFrame(frame); }
    });

    resize();
    if (W >= 2) {
      for (var s = 0; s < 4; s++) spawnPulse();
      raf = requestAnimationFrame(frame);
    }
  }

  function init() {
    wireThemeToggle();
    wireCopy();
    wireNav();
    wireReveal();
    wirePond();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
