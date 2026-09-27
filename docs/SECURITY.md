# Security

PortFlow runs on the developer's own workstation with access to a private
CA and a trust-store install path. We take the following posture:

## Loopback by default

- The proxy binds to `127.0.0.1` and `::1` only. LAN mode is opt-in.
- The management API binds to `127.0.0.1:9280` and also enforces a
  loopback-only check per request.
- Proxy targets must resolve to loopback (`127.0.0.1`, `::1`, `localhost`).
  Anything else is rejected unless the operator explicitly enables
  external targets. This blocks SSRF-style abuse where a bad `Host`
  header would otherwise let a browser reach an internal LAN address
  through PortFlow.

## Certificate authority

- The CA is per-user, generated on first daemon start.
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
- The management API rejects any non-loopback remote address.

## Request logging

- Off by default. When enabled, `Authorization`, `Cookie`, and
  `Set-Cookie` headers are always redacted; request bodies are never
  logged.

## Threats we do NOT defend against

- A local attacker who already has your user account. If they have your
  shell they can trivially issue their own certificates with the CA key.
- Malicious code you deliberately run. PortFlow is a developer tool and
  proxies your development traffic; it is not a sandbox.

## Reporting

Please open a private security advisory on the GitHub repository. Do not
disclose potential vulnerabilities in public issues.
