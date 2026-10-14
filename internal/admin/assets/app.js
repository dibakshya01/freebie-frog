"use strict";

// A custom header proves a same-origin request (AdminGuard requires it for writes).
const RW = { "X-Requested-With": "fetch" };

let STATE = null; // /admin/api/state  — config snapshot
let STATS = null; // /admin/api/stats  — live metrics

/* ---------- helpers ---------- */
function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}
function num(n) { return (n == null ? 0 : n).toLocaleString(); }
function pct(x) { return (x * 100).toFixed(x >= 0.995 || x === 0 ? 0 : 1) + "%"; }
function $(id) { return document.getElementById(id); }
function val(id) { return $(id).value; }
function checked(id) { return $(id).checked; }
function splitCSV(s) { return s.split(",").map(x => x.trim()).filter(Boolean); }

async function getJSON(url) {
  const r = await fetch(url, { headers: RW });
  if (!r.ok) throw new Error(url + " → " + r.status);
  return r.json();
}
async function sendJSON(method, url, body) {
  const opts = { method, headers: { ...RW } };
  if (body !== undefined) { opts.headers["Content-Type"] = "application/json"; opts.body = JSON.stringify(body); }
  const r = await fetch(url, opts);
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || ("HTTP " + r.status));
  return data;
}

function health(ps) {
  if (!ps || !ps.requests) return { cls: "idle", label: "idle" };
  const rate = ps.ok / ps.requests;
  if (rate >= 0.95) return { cls: "ok", label: "healthy " + pct(rate), rate };
  if (rate >= 0.5) return { cls: "warn", label: "degraded " + pct(rate), rate };
  return { cls: "danger", label: "failing " + pct(rate), rate };
}
function badge(h) {
  const b = el("span", "badge " + h.cls);
  b.appendChild(el("span", "bdot"));
  b.appendChild(document.createTextNode(h.label));
  return b;
}

/* ---------- load ---------- */
async function loadState() { try { STATE = await getJSON("/admin/api/state"); applyState(); } catch (e) { console.error(e); } }
async function loadStats() {
  try { STATS = await getJSON("/admin/api/stats"); applyStats(); $("refresh").textContent = "updated " + new Date().toLocaleTimeString(); }
  catch (e) { console.error(e); }
}

function applyState() {
  $("version").textContent = STATE.version || "";
  const pill = $("mode-pill");
  if (STATE.write_enabled) {
    pill.textContent = "management"; pill.classList.add("write");
    $("tab-manage").hidden = false;
  } else {
    pill.textContent = "read-only"; pill.classList.remove("write");
    $("tab-manage").hidden = true;
  }
  renderProviders();
  renderRouting();
  renderManageList();
  fillRoutingJSON();
}
function applyStats() {
  renderTiles();
  renderBars();
  renderRecentMini();
  renderActivity();
  renderProviders(); // health badges depend on stats
}

/* ---------- overview ---------- */
function renderTiles() {
  const root = $("tiles"); root.innerHTML = "";
  const t = (STATS && STATS.totals) || {};
  const recent = (STATS && STATS.recent) || [];
  const rate = t.requests ? t.ok / t.requests : 0;
  let avg = 0;
  if (recent.length) { avg = Math.round(recent.reduce((s, e) => s + (e.duration_ms || 0), 0) / recent.length); }

  const tile = (label, value, sub) => {
    const d = el("div", "tile");
    d.appendChild(el("div", "label", label));
    d.appendChild(el("div", "value", value));
    if (sub) d.appendChild(el("div", "sub", sub));
    root.appendChild(d);
  };
  tile("Requests", num(t.requests || 0), num(t.streamed || 0) + " streamed");
  tile("Success rate", t.requests ? pct(rate) : "—", num(t.failed || 0) + " failed");
  tile("Avg latency", recent.length ? avg + " ms" : "—", "recent window");
  tile("Tokens", num(t.input_tokens || 0) + " / " + num(t.output_tokens || 0), "input / output");
}

function renderBars() {
  const root = $("provider-bars"); root.innerHTML = "";
  const provs = (STATS && STATS.providers) || {};
  const rows = Object.keys(provs).map(name => ({ name: name || "(no route)", ps: provs[name] }))
    .sort((a, b) => b.ps.requests - a.ps.requests);
  if (!rows.length) { root.appendChild(el("p", "empty", "No requests recorded yet.")); return; }
  const max = Math.max(...rows.map(r => r.ps.requests), 1);
  for (const { name, ps } of rows) {
    const row = el("div", "barrow");
    row.appendChild(el("div", "name", name));
    const track = el("div", "bartrack");
    const fill = el("div", "barfill");
    fill.style.width = Math.max(2, Math.round((ps.requests / max) * 100)) + "%";
    track.appendChild(fill);
    row.appendChild(track);
    row.appendChild(el("div", "count", num(ps.requests)));
    row.title = name + " — " + num(ps.requests) + " requests, " + num(ps.ok) + " ok, " +
      num(ps.failed) + " failed, " + (ps.avg_ms || 0) + " ms avg";
    root.appendChild(row);
  }
}

function renderRecentMini() {
  const root = $("recent-mini"); root.innerHTML = "";
  const recent = (STATS && STATS.recent) || [];
  if (!recent.length) { root.appendChild(el("p", "empty", "No requests recorded yet.")); return; }
  const wrap = el("div", "tbl-wrap");
  const table = el("table");
  const tb = el("tbody");
  for (const e of recent.slice(0, 6)) tb.appendChild(activityRow(e));
  table.appendChild(tb); wrap.appendChild(table); root.appendChild(wrap);
}

/* ---------- activity ---------- */
function timeOf(iso) { try { return new Date(iso).toLocaleTimeString(); } catch (_) { return iso; } }
function activityRow(e) {
  const tr = el("tr");
  tr.appendChild(el("td", null, timeOf(e.time)));
  const target = (e.provider ? e.provider : "(no route)") + (e.target_model ? "/" + e.target_model : "");
  const td = el("td");
  td.appendChild(el("span", "mono", e.model || "?"));
  td.appendChild(el("span", "route-arrow", "  →  "));
  td.appendChild(el("span", "mono", target));
  tr.appendChild(td);
  tr.appendChild(el("td", null, e.stream ? "stream" : "unary"));
  const st = el("td");
  const span = el("span", "st " + (e.ok ? "ok" : "err"), e.ok ? "ok " + (e.status || 200) : "err " + (e.status || "")) ;
  st.appendChild(span);
  tr.appendChild(st);
  tr.appendChild(el("td", "num", e.input_tokens ? num(e.input_tokens) : "—"));
  tr.appendChild(el("td", "num", e.output_tokens ? num(e.output_tokens) : "—"));
  tr.appendChild(el("td", "num", (e.duration_ms || 0)));
  return tr;
}
function renderActivity() {
  const tb = $("activity-rows"); tb.innerHTML = "";
  const recent = (STATS && STATS.recent) || [];
  if (!recent.length) {
    const tr = el("tr"); const td = el("td", "empty"); td.colSpan = 7; td.textContent = "No requests recorded yet.";
    tr.appendChild(td); tb.appendChild(tr); return;
  }
  for (const e of recent) tb.appendChild(activityRow(e));
}

/* ---------- providers ---------- */
function renderProviders() {
  const root = $("providers"); if (!root || !STATE) return;
  root.innerHTML = "";
  const list = STATE.providers || [];
  $("provider-count").textContent = "(" + list.length + ")";
  if (!list.length) { root.appendChild(el("p", "empty", "No providers configured.")); return; }
  const stats = (STATS && STATS.providers) || {};
  for (const p of list) {
    const ps = stats[p.name];
    const card = el("div", "prov");
    const head = el("div", "head");
    head.appendChild(el("div", "name", p.name));
    head.appendChild(badge(health(ps)));
    card.appendChild(head);

    const meta = el("div", "row");
    meta.appendChild(el("span", "chip", p.type));
    meta.appendChild(el("span", "chip " + (p.key_configured ? "ok" : "warn"), p.key_configured ? "key set" : "no key"));
    if (p.allow_local) meta.appendChild(el("span", "chip", "allow_local"));
    card.appendChild(meta);

    if (p.base_url) card.appendChild(el("div", "row mono muted", p.base_url));
    const models = (p.models && p.models.length) ? p.models.join(", ") : "(discovered at runtime)";
    const mrow = el("div", "row");
    mrow.appendChild(el("span", "muted", "models: "));
    mrow.appendChild(el("span", "mono", models));
    card.appendChild(mrow);

    if (ps && ps.requests) {
      const kv = el("div", "kv");
      const add = (k, v) => { kv.appendChild(el("div", null, k)); kv.appendChild(el("div", "mono", v)); };
      add("requests", num(ps.requests));
      add("success", num(ps.ok) + " / " + num(ps.requests));
      add("avg latency", (ps.avg_ms || 0) + " ms");
      add("tokens", num(ps.input_tokens) + " / " + num(ps.output_tokens));
      card.appendChild(kv);
      if (ps.last_error) card.appendChild(el("div", "lasterr", "last error: " + ps.last_error));
    }
    root.appendChild(card);
  }
}

/* ---------- routing ---------- */
function renderRouting() {
  const root = $("routing"); if (!root || !STATE) return;
  root.innerHTML = "";
  const r = STATE.routing || {};
  const block = (label, targets) => {
    const d = el("div", "row");
    d.appendChild(el("span", "muted", label + ": "));
    d.appendChild(el("span", "targets", (targets && targets.length) ? targets.join("  →  ") : "(none)"));
    root.appendChild(d);
  };
  block("default", r.default);
  if (r.tiers) for (const t of Object.keys(r.tiers)) block("tier:" + t, r.tiers[t]);
  if (r.aliases) for (const a of Object.keys(r.aliases)) block("alias:" + a, r.aliases[a]);
  if (!(r.default && r.default.length) && !r.tiers && !r.aliases) root.appendChild(el("p", "empty", "No routing configured."));
}

/* ---------- manage (opt-in) ---------- */
function showBanner(msg, cls) {
  const b = $("manage-banner");
  b.className = "banner" + (cls ? " " + cls : "");
  b.textContent = msg; b.hidden = false;
}
function renderManageList() {
  const root = $("manage-provider-list"); if (!root) return;
  root.innerHTML = "";
  const list = (STATE && STATE.providers) || [];
  if (!list.length) { root.appendChild(el("p", "empty", "No providers yet. Add one above.")); return; }
  for (const p of list) {
    const row = el("div", "row");
    row.style.justifyContent = "space-between";
    row.style.borderBottom = "1px solid var(--border)";
    row.style.padding = "8px 0";
    const left = el("div");
    left.appendChild(el("span", "mono", p.name));
    left.appendChild(el("span", "muted", "  " + p.type + (p.base_url ? " · " + p.base_url : "")));
    const btns = el("div", "row");
    const edit = el("button", "btn", "Edit"); edit.onclick = () => editProvider(p);
    const del = el("button", "btn danger", "Delete"); del.onclick = () => deleteProvider(p.name);
    btns.appendChild(edit); btns.appendChild(del);
    row.appendChild(left); row.appendChild(btns);
    root.appendChild(row);
  }
}
function editProvider(p) {
  $("f-name").value = p.name;
  $("f-type").value = p.type || "openai";
  $("f-baseurl").value = p.base_url || "";
  $("f-keyref").value = p.key_ref || "";
  $("f-models").value = (p.models || []).join(", ");
  $("f-allowlocal").checked = !!p.allow_local;
  const q = p.quirks || {};
  $("f-maxcompletion").checked = !!q.use_max_completion_tokens;
  $("f-streamusage").checked = !!q.stream_usage;
  $("f-disableimages").checked = !!q.disable_images;
  $("prov-form-title").textContent = "Edit provider: " + p.name;
  window.scrollTo({ top: 0, behavior: "smooth" });
}
function resetForm() {
  for (const id of ["f-name", "f-baseurl", "f-keyref", "f-models"]) $(id).value = "";
  $("f-type").value = "openai";
  for (const id of ["f-allowlocal", "f-maxcompletion", "f-streamusage", "f-disableimages"]) $(id).checked = false;
  $("prov-form-title").textContent = "Add / edit provider";
}
async function saveProvider() {
  const name = val("f-name").trim();
  if (!name) { showBanner("Provider name is required.", "err"); return; }
  const body = {
    type: val("f-type"),
    base_url: val("f-baseurl").trim(),
    models: splitCSV(val("f-models")),
    allow_local: checked("f-allowlocal"),
    quirks: {
      use_max_completion_tokens: checked("f-maxcompletion"),
      stream_usage: checked("f-streamusage"),
      disable_images: checked("f-disableimages"),
    },
  };
  const ref = val("f-keyref").trim();
  if (ref) body.api_key_ref = ref;
  try {
    await sendJSON("PUT", "/admin/api/providers/" + encodeURIComponent(name), body);
    showBanner("Saved provider “" + name + "”. Restart `freebie serve` to apply.", "ok");
    resetForm();
    await loadState();
  } catch (e) { showBanner("Save failed: " + e.message, "err"); }
}
async function deleteProvider(name) {
  if (!window.confirm("Delete provider “" + name + "” from config.json?")) return;
  try {
    await sendJSON("DELETE", "/admin/api/providers/" + encodeURIComponent(name));
    showBanner("Deleted provider “" + name + "”. Restart `freebie serve` to apply.", "ok");
    await loadState();
  } catch (e) { showBanner("Delete failed: " + e.message, "err"); }
}
function fillRoutingJSON() {
  const ta = $("routing-json"); if (!ta || !STATE) return;
  if (document.activeElement === ta) return; // don't clobber while editing
  const r = STATE.routing || {};
  ta.value = JSON.stringify({ default: r.default || [], tiers: r.tiers || {}, aliases: r.aliases || {} }, null, 2);
}
async function saveRouting() {
  let rt;
  try { rt = JSON.parse(val("routing-json")); }
  catch (e) { showBanner("Invalid JSON: " + e.message, "err"); return; }
  try {
    await sendJSON("PUT", "/admin/api/routing", rt);
    showBanner("Saved routing. Restart `freebie serve` to apply.", "ok");
    await loadState();
  } catch (e) { showBanner("Save failed: " + e.message, "err"); }
}

/* ---------- tabs + theme ---------- */
function wireTabs() {
  const tabs = document.querySelectorAll(".tab");
  tabs.forEach(t => t.addEventListener("click", () => {
    tabs.forEach(x => x.setAttribute("aria-selected", String(x === t)));
    document.querySelectorAll(".panel").forEach(p => { p.hidden = true; });
    $("panel-" + t.dataset.tab).hidden = false;
  }));
}
function wireTheme() {
  $("theme").addEventListener("click", () => {
    const cur = document.documentElement.getAttribute("data-theme");
    const dark = cur ? cur === "dark" : window.matchMedia("(prefers-color-scheme: dark)").matches;
    const next = dark ? "light" : "dark";
    document.documentElement.setAttribute("data-theme", next);
    try { localStorage.setItem("ff-admin-theme", next); } catch (_) {}
  });
}

function init() {
  wireTabs();
  wireTheme();
  $("prov-save").addEventListener("click", saveProvider);
  $("prov-reset").addEventListener("click", resetForm);
  $("routing-save").addEventListener("click", saveRouting);
  loadState();
  loadStats();
  setInterval(loadStats, 3000);
}
init();
