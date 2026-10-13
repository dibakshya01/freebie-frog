"use strict";

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}

function renderServer(s) {
  const root = document.getElementById("server");
  root.innerHTML = "";
  const kv = el("div", "kv");
  const add = (k, v) => { kv.appendChild(el("div", null, k)); kv.appendChild(el("div", null, v)); };
  add("Listen address", s.addr);
  add("Ingress auth", s.auth_required ? "required" : "none (loopback trust)");
  root.appendChild(kv);
}

function renderProviders(list) {
  const root = document.getElementById("providers");
  root.innerHTML = "";
  document.getElementById("provider-count").textContent = "(" + list.length + ")";
  if (!list.length) { root.appendChild(el("p", "muted", "No providers configured.")); return; }
  for (const p of list) {
    const card = el("div", "prov");
    card.appendChild(el("div", "name", p.name));
    const meta = el("div", "row");
    meta.appendChild(el("span", "chip", p.type));
    meta.appendChild(el("span", "chip " + (p.key_configured ? "ok" : "warn"),
      p.key_configured ? "key set" : "no key"));
    if (p.allow_local) meta.appendChild(el("span", "chip", "allow_local"));
    card.appendChild(meta);
    if (p.base_url) card.appendChild(el("div", "row mono muted", p.base_url));
    const models = (p.models && p.models.length) ? p.models.join(", ") : "(discovered at runtime)";
    const mrow = el("div", "row");
    mrow.appendChild(el("span", "muted", "models: "));
    mrow.appendChild(el("span", "mono", models));
    card.appendChild(mrow);
    root.appendChild(card);
  }
}

function renderRouting(r) {
  const root = document.getElementById("routing");
  root.innerHTML = "";
  const block = (label, targets) => {
    const d = el("div", "row");
    d.appendChild(el("span", "muted", label + ": "));
    d.appendChild(el("span", "targets", (targets && targets.length) ? targets.join("  →  ") : "(none)"));
    root.appendChild(d);
  };
  block("default", r.default);
  if (r.tiers) for (const t of Object.keys(r.tiers)) block("tier:" + t, r.tiers[t]);
  if (r.aliases) for (const a of Object.keys(r.aliases)) block("alias:" + a, r.aliases[a]);
}

async function load() {
  try {
    const res = await fetch("/admin/api/state", { headers: { "X-Requested-With": "fetch" } });
    const s = await res.json();
    document.getElementById("version").textContent = s.version || "";
    renderServer(s.server);
    renderProviders(s.providers || []);
    renderRouting(s.routing || {});
  } catch (e) {
    document.getElementById("server").textContent = "Failed to load state: " + e;
  }
}

load();
