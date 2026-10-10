# Changelog

All notable changes to PortFlow will be documented here. Format based on
[Keep a Changelog](https://keepachangelog.com), versioning follows
[Semantic Versioning](https://semver.org).

## [Unreleased]

### Security
- Diagnostic pages HTML-escape the `Host` header and detail text (reflected
  XSS) and send CSP / `nosniff`; the dashboard no longer uses `innerHTML`
  (stored XSS through project names).
- Management API: DNS-rebinding (`Host`) and CSRF (`X-PortFlow` + `Origin`)
  protection; previously any web page could POST to `127.0.0.1:9280`.
- Registration is limited to `.test` / `.localhost` / `.local`, validated
  project names and loopback targets; body size capped.
- Health probes no longer dial non-loopback targets.
- Proxy listen addresses must be loopback unless `--lan`.

### Changed
- `portflow add` registers with HTTPS by default and prints the real URL
  including the proxy port (`open`, `list`, `info` too).
- `trust` / `untrust` ask for confirmation (`-y` to skip); `untrust` and
  `doctor` no longer create a CA or data directory.
- `up` is idempotent and exits non-zero when a service fails.
- Proxy: single shared ReverseProxy, client `X-Forwarded-*` replaced,
  `Host` rewritten to the target (`--preserve-host` to keep the original),
  plain-HTTP requests to TLS services redirect to HTTPS.
- Default listeners are dual-stack loopback (`127.0.0.1` and `::1`).
- `detect` offers only HTTP services, skips PortFlow's own and registered
  ports, makes names unique, and marks all-interface listeners.

### Fixed
- Daemon hung forever (API up, nothing proxied) when a proxy port could not
  be bound; it now exits with a hint.
- Trust-store probe ran on every dashboard poll; now cached for 30 s.
- `project init` produced invalid hostnames from directory names with spaces.
- Dev servers bound only to `::1` (Node 17+) were unreachable from the
  default `127.0.0.1` target.

### Added (new)
- `portflow logs` and a dashboard panel: optional in-memory request log.
- `daemon start --lan`, `--preserve-host`, `--request-log`,
  `--allow-external-targets`; `detect --all`.
- Dashboard: inspect and edit actions, CA-not-trusted banner.

### Added
- `portflow info <domain>` — show a single service's full record plus a
  live TCP probe to the target.
- `portflow detect --docker` — also scan published Docker containers.
- `portflow doctor` now warns when the local CA is close to expiry.
- `contrib/systemd/`, `contrib/launchd/`, `contrib/windows/` — ready-to-use
  system service examples with sandboxing / NSSM instructions.
- Dashboard: project filter dropdown (remembers choice in localStorage).
- Daemon start pre-flight: refuses to start if another daemon is already
  answering `/health` on the API address.
- `.gitattributes` + `.editorconfig` for consistent LF line endings.

### Fixed
- Dependency-free Docker discovery (dropped `github.com/docker/docker`
  transitive compile failures on Linux CI).
- IPv6-safe TCP probe in health loop (`net.JoinHostPort`).
- Proxy test closure referenced `upstream` before its `:=` declaration.
- macOS CI: disable CGO so gopsutil's clang VLA warnings don't fail the
  build.

## [0.1.0] — 2026-09-27

Initial spike.

### Added
- Go daemon (`portflow daemon start`) hosting registry, proxy, and API.
- SQLite service registry with hostname uniqueness and status tracking.
- Local development CA with per-domain leaf certs (90-day expiry, auto-rotate).
- Reverse proxy on `127.0.0.1:80/443` — HTTP/1.1, HTTP/2, WebSockets, SSE.
- OS trust-store adapters (Windows/macOS/Linux) via `portflow trust`.
- Managed-block hosts-file editor.
- CLI: `add`, `remove`, `list`, `status`, `detect`, `open`, `project init`,
  `up`, `trust`, `untrust`, `doctor`, `daemon start|stop`.
- `portflow.yml` project files.
- Loopback process discovery via gopsutil.
- Optional Docker discovery.
- Embedded web dashboard at `http://127.0.0.1:9280/`.
- Tauri desktop shell (thin wrapper around the built-in dashboard).
- README (EN + TR), SECURITY, PRIVACY, ARCHITECTURE, CONTRIBUTING.

### Known limitations
- No system-service installer yet — the daemon must be started manually.
- Docker discovery is read-only (no auto-registration of container ports).
- TCP aliases (non-HTTP services) not implemented.
- No request log UI yet.
