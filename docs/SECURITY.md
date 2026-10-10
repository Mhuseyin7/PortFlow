# Security

PortFlow runs on the developer's own workstation with access to a private
CA and a trust-store install path. We take the following posture:

## Loopback by default

- The proxy binds to `127.0.0.1` and `::1` only. LAN mode (`--lan`) is
  opt-in and prints a warning at startup.
- The management API binds to `127.0.0.1:9280`, is never exposed by
  `--lan`, and enforces a loopback-only check per request.
- The API rejects requests whose `Host` header is not a loopback name
  (DNS rebinding), and state-changing requests must carry the `X-PortFlow`
  header and, if an `Origin` is sent, a same-origin one (CSRF). A web page
  in your browser therefore cannot register services or stop the daemon.
- The health checker never connects to non-loopback targets, so it cannot
  be used as a port scanner.
- Proxy targets must resolve to loopback (`127.0.0.1`, `::1`, `localhost`).
  Anything else is rejected unless the operator explicitly enables
  external targets. This blocks SSRF-style abuse where a bad `Host`
  header would otherwise let a browser reach an internal LAN address
  through PortFlow.

## Certificate authority

- The CA is per-user, generated on first daemon start.
- The CA carries critical X.509 name constraints limiting it to `.test`,
  `.localhost` and `.local`; even a leaked key cannot vouch for a real site.
- The CA private key is written mode `0600` under `~/.portflow/ca/`.
- Leaf certificates are issued only for hostnames that are registered
  in the local SQLite registry (never for arbitrary SNI).
- Leaf certs are short-lived (90 days) and rotated a week before expiry.
- The CA is **never** installed into the OS trust store automatically.
  It requires an explicit `portflow trust`, which runs the platform's
  standard trust tool (`certutil`, `security add-trusted-cert`,
  `update-ca-certificates` / `update-ca-trust`) with the standard
  UAC / sudo prompt.

## Hosts file

- PortFlow edits `/etc/hosts` (or the Windows equivalent) inside a
  clearly marked block delimited by `# BEGIN PortFlow` / `# END PortFlow`.
- Writes are atomic (temp file + rename in the same directory).
- Entries outside the block are never touched.

## Input validation

- Hostnames are validated against `[a-z0-9.-]+`, length ≤ 253,
  no leading/trailing dots, no `..` segments. Path traversal via the
  hostname is impossible because filenames are derived from the
  validated hostname only.
- Registration accepts only `.test`, `.localhost` and `.local` names, so a
  caller cannot hijack a real domain in the hosts file.
- Project names and target hosts are validated; request bodies are capped
  at 64 KiB and unknown JSON fields are rejected.
- The management API rejects any non-loopback remote address.
- Diagnostic pages HTML-escape every value (the `Host` header is
  attacker-controlled) and are served with a restrictive CSP; the
  dashboard builds its DOM with `textContent`, never `innerHTML`.

## Request logging

- Off by default and held in memory only (last 500 requests). Headers and
  bodies are never recorded; only method, host, path, status and duration.
  Query values with sensitive-looking keys or opaque token-like values are
  replaced with `[redacted]`.

## Threats we do NOT defend against

- A local attacker who already has your user account. If they have your
  shell they can trivially issue their own certificates with the CA key.
- Malicious code you deliberately run. PortFlow is a developer tool and
  proxies your development traffic; it is not a sandbox.

## Reporting

Please open a private security advisory on the GitHub repository. Do not
disclose potential vulnerabilities in public issues.
