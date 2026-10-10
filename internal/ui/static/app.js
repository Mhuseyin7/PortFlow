"use strict";

const $ = (s) => document.querySelector(s);

// Build DOM with textContent only: service names, projects and request paths
// are data, never markup.
function el(tag, attrs = {}, ...kids) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") n.className = v;
    else if (k === "text") n.textContent = v;
    else if (k.startsWith("data-")) n.setAttribute(k, v);
    else n[k] = v;
  }
  for (const kid of kids) if (kid != null) n.append(kid);
  return n;
}

async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json", "X-PortFlow": "1" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!res.ok) {
    throw new Error((data && data.error) || (typeof data === "string" && data) || res.statusText);
  }
  return data;
}

// --- proxy ports, so links work when the daemon is not on 80/443 ----------
let ports = { http: "", https: "" };
const firstPort = (spec) => {
  const first = String(spec || "").split(",")[0].trim();
  const i = first.lastIndexOf(":");
  return i >= 0 ? first.slice(i + 1) : "";
};

function urlFor(svc) {
  const https = !!svc.tls;
  const port = https ? ports.https : ports.http;
  const def = https ? "443" : "80";
  return `${https ? "https" : "http"}://${svc.hostname}${port && port !== def ? ":" + port : ""}`;
}

// --- state ------------------------------------------------------------------
let projectFilter = "";
try { projectFilter = localStorage.getItem("portflow:filter") || ""; } catch {}
let services = [];
const expanded = new Set();
let editing = null; // the service being edited, or null

// --- status banner ----------------------------------------------------------
async function refreshStatus() {
  try {
    const s = await api("GET", "/api/status");
    ports = { http: firstPort(s.proxy_http), https: firstPort(s.proxy_https) };
    $("#status").textContent = `v${s.version} · ${s.services} service(s) · up ${s.uptime}`;
    const banner = $("#banner");
    const notes = [];
    if (!s.ca_trusted) notes.push("The local CA is not trusted yet — run `portflow trust` in a terminal so browsers accept https:// addresses.");
    if (s.allow_external_targets) notes.push("External (non-loopback) proxy targets are enabled.");
    banner.hidden = notes.length === 0;
    banner.textContent = notes.join(" ");
    const toggle = $("#log-toggle");
    if (toggle && document.activeElement !== toggle) toggle.checked = !!s.request_log;
  } catch {
    $("#status").textContent = "daemon offline";
  }
}

// --- services ---------------------------------------------------------------
function renderFilter(projects) {
  const f = $("#filter");
  f.replaceChildren(...["", ...projects].map((p) =>
    el("option", { value: p, text: p || "all projects", selected: p === projectFilter })));
}

function detailRow(s) {
  const rows = [
    ["hostname", s.hostname], ["url", urlFor(s)], ["target", `${s.target_host}:${s.target_port}`],
    ["project", s.project || "(none)"], ["protocol", s.protocol], ["tls", String(s.tls)],
    ["status", s.status], ["created", new Date(s.created_at).toLocaleString()],
    ["updated", new Date(s.updated_at).toLocaleString()],
  ];
  const dl = el("dl", { class: "details" });
  for (const [k, v] of rows) dl.append(el("dt", { text: k }), el("dd", { text: v }));
  return dl;
}

function serviceRow(s) {
  const url = urlFor(s);
  const row = el("div", { class: "svc" },
    el("div", { class: "host" }, el("a", { href: url, target: "_blank", rel: "noopener noreferrer", text: url })),
    el("div", { class: "port", text: `→ ${s.target_host}:${s.target_port}` }),
    el("div", {}, el("span", { class: `badge ${s.status}`, text: s.status })),
    el("div", { class: "actions" },
      el("button", { class: "ghost", text: "copy", "data-act": "copy", "data-host": s.hostname }),
      el("button", { class: "ghost", text: expanded.has(s.hostname) ? "hide" : "inspect", "data-act": "inspect", "data-host": s.hostname }),
      el("button", { class: "ghost", text: "edit", "data-act": "edit", "data-host": s.hostname }),
      el("button", { class: "danger", text: "remove", "data-act": "remove", "data-host": s.hostname })));
  const wrap = el("div", { class: "svc-wrap" }, row);
  if (expanded.has(s.hostname)) wrap.append(detailRow(s));
  return wrap;
}

function renderServices() {
  const root = $("#services");
  if (services.length === 0) {
    root.replaceChildren(el("div", { class: "muted", text: "no services yet — add one above" }));
    renderFilter([]);
    return;
  }
  const groups = new Map();
  for (const s of services) {
    const key = s.project || "(no project)";
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(s);
  }
  renderFilter([...groups.keys()]);
  const out = [];
  for (const [name, list] of groups) {
    if (projectFilter && projectFilter !== name) continue;
    out.push(el("div", { class: "group" }, el("h3", { text: name }), ...list.map(serviceRow)));
  }
  root.replaceChildren(...out);
}

async function refreshServices() {
  try {
    services = (await api("GET", "/api/services")) || [];
    renderServices();
  } catch (e) {
    $("#services").replaceChildren(el("div", { class: "err", text: e.message }));
  }
}

// --- edit / add form --------------------------------------------------------
function setEditing(svc) {
  editing = svc;
  const f = $("#add-form");
  f.elements.hostname.value = svc ? svc.hostname : "";
  f.elements.port.value = svc ? svc.target_port : "";
  f.elements.project.value = svc ? svc.project : "";
  f.elements.tls.checked = svc ? svc.tls : true;
  $("#form-title").textContent = svc ? `Edit ${svc.hostname}` : "Register a service";
  $("#submit-btn").textContent = svc ? "Save" : "Add";
  $("#cancel-edit").hidden = !svc;
  if (svc) f.elements.hostname.focus();
}

$("#cancel-edit").addEventListener("click", () => { setEditing(null); $("#add-err").textContent = ""; });

$("#add-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const f = new FormData(e.target);
  const err = $("#add-err");
  err.textContent = "";
  err.className = "err";
  const body = {
    hostname: String(f.get("hostname")).trim(),
    target_port: Number(f.get("port")),
    target_host: "127.0.0.1",
    project: String(f.get("project") || "").trim(),
    tls: f.get("tls") === "on",
  };
  try {
    let res;
    if (editing) {
      // The API has no update call: replace, and put the original back if the
      // replacement is rejected.
      const old = editing;
      await api("DELETE", `/api/services/${encodeURIComponent(old.hostname)}`);
      try {
        res = await api("POST", "/api/services", body);
      } catch (e2) {
        try {
          await api("POST", "/api/services", {
            hostname: old.hostname, target_port: old.target_port, target_host: old.target_host,
            project: old.project, tls: old.tls,
          });
        } catch {}
        throw e2;
      }
    } else {
      res = await api("POST", "/api/services", body);
    }
    setEditing(null);
    if (res && res.warnings && res.warnings.length) {
      err.className = "warn";
      err.textContent = res.warnings.join(" ");
    }
    await refreshServices();
  } catch (ex) {
    err.textContent = ex.message;
    await refreshServices();
  }
});

// --- row actions --------------------------------------------------------------
document.addEventListener("click", async (e) => {
  const b = e.target.closest("button[data-act]");
  if (!b) return;
  const host = b.dataset.host;
  const svc = services.find((s) => s.hostname === host);
  switch (b.dataset.act) {
    case "copy":
      try { await navigator.clipboard.writeText(urlFor(svc)); b.textContent = "copied"; }
      catch { b.textContent = "failed"; }
      setTimeout(() => { b.textContent = "copy"; }, 1000);
      break;
    case "inspect":
      if (expanded.has(host)) expanded.delete(host); else expanded.add(host);
      renderServices();
      break;
    case "edit":
      setEditing(svc);
      window.scrollTo({ top: 0, behavior: "smooth" });
      break;
    case "remove":
      if (!confirm(`Remove ${host}?`)) return;
      try { await api("DELETE", `/api/services/${encodeURIComponent(host)}`); expanded.delete(host); }
      catch (ex) { alert(ex.message); }
      refreshServices();
      break;
  }
});

document.addEventListener("change", (e) => {
  if (e.target.id === "filter") {
    projectFilter = e.target.value;
    try { localStorage.setItem("portflow:filter", projectFilter); } catch {}
    renderServices();
  }
  if (e.target.id === "log-toggle") {
    api("POST", `/api/requests/${e.target.checked ? "enable" : "disable"}`).then(refreshRequests, (ex) => alert(ex.message));
  }
});

// --- request log --------------------------------------------------------------
async function refreshRequests() {
  const box = $("#requests");
  try {
    const r = await api("GET", "/api/requests");
    $("#log-toggle").checked = r.enabled;
    if (!r.entries.length) {
      box.replaceChildren(el("div", { class: "muted", text: r.enabled ? "waiting for requests…" : "recording is off" }));
      return;
    }
    const rows = r.entries.slice().reverse().slice(0, 100).map((x) => el("tr", {},
      el("td", { text: new Date(x.time).toLocaleTimeString() }),
      el("td", { text: x.method }),
      el("td", { text: x.host }),
      el("td", { class: x.status >= 500 ? "bad" : x.status >= 400 ? "warn" : "", text: String(x.status) }),
      el("td", { text: `${x.duration_ms.toFixed(1)} ms` }),
      el("td", { class: "path", text: x.path })));
    box.replaceChildren(el("table", {}, el("tbody", {}, ...rows)));
  } catch {
    box.replaceChildren();
  }
}

$("#log-clear").addEventListener("click", () => {
  api("DELETE", "/api/requests").then(refreshRequests, (ex) => alert(ex.message));
});

refreshStatus();
refreshServices();
refreshRequests();
setInterval(refreshStatus, 3000);
setInterval(refreshServices, 5000);
setInterval(refreshRequests, 3000);
