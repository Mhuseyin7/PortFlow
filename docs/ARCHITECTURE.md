# PortFlow architecture

PortFlow is one Go binary that plays two roles:

- **`portflow` CLI** — a thin cobra wrapper that talks to the daemon over an
  HTTP API bound to `127.0.0.1`.
- **`portflow daemon`** — a long-running process that owns the registry,
  the reverse proxy, the local CA, and the hosts-file adapter.

```
             +-------------------+
   CLI  <--> |   management API  | 127.0.0.1:9280
             |   built-in web UI |
             +---------+---------+
                       |
                       v
             +---------+---------+
             |     Registry      | SQLite @ ~/.portflow/registry.db
             +---------+---------+
                       |
                       v
             +---------+---------+       +----------------+
Browser  ->  |   Reverse proxy   |  ->   | Your dev server|
             |   127.0.0.1:80/443|       |  (localhost)   |
             +---------+---------+       +----------------+
                       |
                       v
             +---------+---------+
             |   Local CA + certs|  ~/.portflow/ca, ~/.portflow/certs
             +-------------------+
```

## Packages

| Package                        | Purpose |
|--------------------------------|---------|
| `cmd/portflow`                 | cobra CLI. Talks to the daemon over HTTP. |
| `internal/config`              | Data-dir resolution, atomic file writes, `portflow.yml`. |
| `internal/registry`            | SQLite service store. Single source of truth for `hostname → target`. |
| `internal/tls`                 | Local development CA (`ca.go`) and on-disk leaf cert cache (`store.go`). |
| `internal/trust`               | Per-OS trust store install/uninstall (Windows/macOS/Linux). |
| `internal/hosts`               | Managed-block editor for the OS hosts file. |
| `internal/proxy`               | Hostname-routed reverse proxy: HTTP/1.1, HTTP/2, WebSocket, SSE. |
| `internal/discovery`           | Process (gopsutil) and Docker discovery. |
| `internal/api`                 | Loopback-only management HTTP API + JSON schemas. |
| `internal/ui`                  | Embedded HTML/CSS/JS dashboard served by the daemon. |
| `internal/daemon`              | Composes everything into one runnable process; health loop. |

## Request lifecycle

1. Browser resolves `app.shop.test`. Because the hosts adapter has written
   `127.0.0.1 app.shop.test`, the request lands on the local proxy.
2. Proxy reads SNI (for HTTPS) or `Host` (for HTTP), looks the hostname up
   in the registry, and refuses if it's unknown (with an HTML diagnostic).
3. For HTTPS, the cert store returns (or mints) a leaf cert signed by
   PortFlow's local CA.
4. `httputil.ReverseProxy` forwards to `127.0.0.1:<port>` with the correct
   `X-Forwarded-*` headers and a rewritten `Host`.
5. WebSocket upgrades are hijacked and tunneled bidirectionally.

## Storage layout

```
~/.portflow/
  registry.db                  SQLite (WAL)
  ca/
    portflow-ca.pem            CA certificate (0644)
    portflow-ca.key            CA private key (0600)
  certs/
    app.shop.test.pem          leaf cert (chain incl. CA)
    app.shop.test.key          leaf key (0600)
```

Everything is per-user. PortFlow never writes to shared system state
except the hosts file and (only via explicit `portflow trust`) the OS
trust store.

## Security posture

See [SECURITY.md](SECURITY.md).
