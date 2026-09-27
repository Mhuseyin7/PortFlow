# PortFlow

Stable local HTTPS domains and a central service registry for local
development. Open source — built by **[muhammedkoca.com.tr](https://muhammedkoca.com.tr)**.

Instead of remembering:

```
localhost:3000
localhost:5173
localhost:8000
```

use:

```
https://app.shop.test
https://admin.shop.test
https://api.shop.test
```

PortFlow is strictly a **local development** tool. It never exposes your
services to the public internet, and it never modifies your OS trust
store without an explicit command.

## Features

- Loopback-only reverse proxy on `127.0.0.1:80/443` (HTTP/1.1, HTTP/2,
  WebSockets, SSE).
- Per-user local Certificate Authority, on-demand leaf certs, 90-day
  auto-rotation.
- Explicit OS trust-store install/uninstall (Windows / macOS / Linux).
- SQLite service registry with hostname uniqueness and health tracking.
- Managed-block hosts-file editor with atomic writes.
- Process discovery (Next.js, Vite, FastAPI, Django, Express, …) and
  optional Docker discovery.
- Embedded web dashboard + Tauri 2 desktop shell.
- Clean cobra-based CLI.

## Quick start

```bash
go build -o portflow .
sudo ./portflow daemon start       # or Admin PowerShell on Windows
./portflow trust
./portflow add api.shop.test 8000
./portflow open api.shop.test
```

## Docs

Full documentation lives in the Turkish-first [README.md](README.md) and
the [`docs/`](docs/) folder:

- [ARCHITECTURE](docs/ARCHITECTURE.md)
- [SECURITY](docs/SECURITY.md)
- [PRIVACY](docs/PRIVACY.md)
- [CONTRIBUTING](docs/CONTRIBUTING.md)
- [CHANGELOG](docs/CHANGELOG.md)

## Credits

Built and maintained by **[Muhammed Koca](https://muhammedkoca.com.tr)**
([@Mhuseyin7](https://github.com/Mhuseyin7)).

## License

[MIT](LICENSE)
