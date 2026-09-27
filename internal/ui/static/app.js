const $ = (s) => document.querySelector(s);

async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!res.ok) throw new Error(data?.error || res.statusText);
  return data;
}

async function refreshStatus() {
  try {
    const s = await api("GET", "/api/status");
    $("#status").textContent = `v${s.version} · ${s.services} service(s) · up ${s.uptime}`;
  } catch (e) {
    $("#status").textContent = "daemon offline";
  }
}

function urlFor(svc) {
  const scheme = svc.tls ? "https" : "http";
  return `${scheme}://${svc.hostname}`;
}

async function refreshServices() {
  const el = $("#services");
  el.innerHTML = "";
  const list = await api("GET", "/api/services");
  if (!list || list.length === 0) {
    el.innerHTML = `<div class="muted">no services yet — add one above</div>`;
    return;
  }
  const groups = {};
  for (const s of list) {
    const key = s.project || "(no project)";
    (groups[key] = groups[key] || []).push(s);
  }
  for (const [name, svcs] of Object.entries(groups)) {
    const g = document.createElement("div");
    g.className = "group";
    g.innerHTML = `<h3>${name}</h3>`;
    for (const s of svcs) {
      const row = document.createElement("div");
      row.className = "svc";
      row.innerHTML = `
        <div class="host"><a href="${urlFor(s)}" target="_blank">${urlFor(s)}</a></div>
        <div class="port">→ ${s.target_host}:${s.target_port}</div>
        <div><span class="badge ${s.status}">${s.status}</span></div>
        <div>
          <button class="ghost" data-copy="${urlFor(s)}">copy</button>
          <button class="danger" data-del="${s.hostname}">remove</button>
        </div>`;
      g.appendChild(row);
    }
    el.appendChild(g);
  }
}

document.addEventListener("click", async (e) => {
  const copy = e.target.dataset.copy;
  const del = e.target.dataset.del;
  if (copy) {
    await navigator.clipboard.writeText(copy);
    e.target.textContent = "copied";
    setTimeout(() => (e.target.textContent = "copy"), 1000);
  }
  if (del) {
    if (!confirm(`Remove ${del}?`)) return;
    await api("DELETE", `/api/services/${encodeURIComponent(del)}`);
    refreshServices();
  }
});

$("#add-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const f = new FormData(e.target);
  $("#add-err").textContent = "";
  try {
    await api("POST", "/api/services", {
      hostname: f.get("hostname"),
      target_port: Number(f.get("port")),
      target_host: "127.0.0.1",
      project: f.get("project") || "",
      tls: f.get("tls") === "on",
    });
    e.target.reset();
    refreshServices();
  } catch (err) {
    $("#add-err").textContent = err.message;
  }
});

refreshStatus();
refreshServices();
setInterval(refreshStatus, 3000);
setInterval(refreshServices, 5000);
