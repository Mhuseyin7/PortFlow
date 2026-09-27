# PortFlow

> Local development ports için stable HTTPS domains ve central service registry.
> Open source — **[muhammedkoca.com.tr](https://muhammedkoca.com.tr)** tarafından geliştirilmiştir.

---

Şunları ezberlemek yerine:

```
localhost:3000
localhost:5173
localhost:8000
```

şunları kullanın:

```
https://app.shop.test
https://admin.shop.test
https://api.shop.test
```

PortFlow, local development ortamınızdaki dağınık portları **stable HTTPS
domain'lere** çeviren, tamamen **local-first** bir developer tool'dur.
Public tunnel değildir. Cloud'a bir şey göndermez. Sadece kendi
makinenizde çalışır.

- İngilizce README: [README_EN.md](README_EN.md) *(kısaltılmış)*

---

## Özellikler

- **Local Reverse Proxy** — Hostname bazlı routing ile `127.0.0.1:80/443`
  üzerinden HTTP/1.1, HTTP/2, WebSocket ve Server-Sent Events desteği.
- **Otomatik Local HTTPS** — Kendi local Certificate Authority'sini
  oluşturur, her domain için ondan **leaf certificate** signler ve 90
  günde bir otomatik rotate eder.
- **OS Trust Store Integration** — Tek komutla (`portflow trust`) CA'yı
  Windows / macOS / Linux trust store'una **explicit approval** ile
  install eder. Silent install yapmaz.
- **SQLite Registry** — Bütün `hostname → local port` mapping'leri tek
  bir local database'de tutulur.
- **Hosts File Adapter** — İşaretli bir block içinde `/etc/hosts`
  dosyanıza atomic write yapar; senin yazdığın diğer satırlara asla
  dokunmaz.
- **Process Discovery** — `portflow detect` ile makinenizdeki dev
  server'ları (Next.js, Vite, FastAPI, Django, Express, Rails,
  Storybook) framework guess'iyle bulur.
- **Optional Docker Discovery** — Local Docker daemon varsa published
  container port'larını listeler.
- **Built-in Web Dashboard** — Daemon üzerinde `http://127.0.0.1:9280`
  adresinde çalışan embedded dashboard.
- **Tauri Desktop Shell** — Aynı dashboard'un native window'lu versiyonu.
- **CLI** — cobra tabanlı, script-friendly command set.

---

## Bilinçli olarak **olmayan** şeyler

Bu tool'un ne olmadığını bilmek, ne olduğunu bilmek kadar önemli:

- ❌ **Public tunnel değildir** (ngrok / Cloudflare Tunnel alternatifi değil).
- ❌ **Production reverse proxy değildir** (Nginx / Caddy / Traefik
  yerine geçmez).
- ❌ **Ingress controller değildir**.
- ❌ **Network-wide DNS server değildir**.
- ❌ **Generic Nginx GUI değildir**.

PortFlow strict biçimde **local development** için tasarlanmıştır.

---

## Quick Start

```bash
# 1. Build
go build -o portflow .

# 2. Daemon'u başlat (:80 ve :443 için elevation gerekir)
sudo ./portflow daemon start           # Linux / macOS
./portflow daemon start                 # Windows: terminal Admin olsun

# Ya da elevation olmadan dev modu:
make dev                                # 8080 / 8443 üzerinde

# 3. Local CA'yı bir defalık trust store'a install et
./portflow trust

# 4. Bir service register et
./portflow add api.shop.test 8000

# 5. Browser'da aç
./portflow open api.shop.test
```

Sonra kendi dev server'ınızı port 8000'de başlatın. `https://api.shop.test`
artık **trusted**.

---

## CLI Reference

```
portflow add <domain> <port>       yeni bir service register et
portflow remove <domain>           service'i kaldır
portflow list                      bütün service'leri listele (project bazlı)
portflow status                    daemon durumu
portflow detect [--register]       loopback üzerindeki dev server'ları tara
portflow open <domain>             domain'i browser'da aç
portflow project init              starter portflow.yml oluştur
portflow up [-f portflow.yml]      project file'daki her şeyi register et
portflow trust                     local CA'yı OS trust store'a install et
portflow untrust                   local CA'yı trust store'dan kaldır
portflow doctor                    install ve environment tanısı
portflow daemon start|stop         daemon lifecycle
```

---

## Project File

Repo'nuzun kök dizinine bir `portflow.yml` bırakın:

```yaml
version: 1
project: shop
services:
  app:
    domain: app.shop.test
    port: 3000
  api:
    domain: api.shop.test
    port: 8000
  admin:
    domain: admin.shop.test
    port: 5173
```

Sonra:

```bash
portflow up
```

---

## Ports & Elevation

PortFlow default olarak `:80` ve `:443` port'larına bind olur; böylece
browser adres çubuğunuz temiz kalır. Bu port'lar Linux/macOS'ta root,
Windows'ta Administrator ister.

Elevation istemiyorsanız:

```bash
./portflow daemon start --http 127.0.0.1:8080 --https 127.0.0.1:8443
```

Sonra `https://app.shop.test:8443` şeklinde kullanın.

---

## Security Model

- Proxy ve management API sadece **loopback**'e bind olur. LAN mode
  opt-in'dir.
- Proxy target'ları default olarak sadece `127.0.0.1`'e izin verir —
  hostile bir `Host` header ile SSRF yapılamaz.
- Local CA private key `~/.portflow/ca/` altında **mode 0600** ile durur.
- Leaf certificate'ler yalnızca **registry'de kayıtlı** hostname'ler için
  imzalanır. Arbitrary SNI ile cert alınamaz.
- CA **kesinlikle silent install olmaz** — `portflow trust` explicit
  komutu ister, ve platform'un standart trust tool'unu (`certutil`,
  `security add-trusted-cert`, `update-ca-certificates` / `update-ca-trust`)
  standart UAC / sudo prompt'uyla çalıştırır.
- Hosts file editing atomic'tir ve `# BEGIN PortFlow` / `# END PortFlow`
  marker'larıyla sınırlanmış bir block içinde yapılır.

Detaylı security notes: [docs/SECURITY.md](docs/SECURITY.md).

---

## Privacy

- Telemetry **yoktur**. Analytics yoktur. Crash reporter yoktur. Update
  ping'i yoktur.
- Hiçbir external server'a data gönderilmez. Zaten PortFlow-owned
  server yoktur.
- Bütün state kendi home directory'nizde: `~/.portflow/`.
- Kaldırmak için: `portflow untrust && portflow daemon stop && rm -rf ~/.portflow`.

Full policy: [docs/PRIVACY.md](docs/PRIVACY.md).

---

## Architecture

Tek bir Go binary, iki role sahip:

- **`portflow` CLI** — cobra wrapper; daemon'un loopback HTTP API'siyle
  konuşur.
- **`portflow daemon`** — registry + reverse proxy + local CA + hosts
  adapter'ı taşıyan long-running process.

```
                +-------------------+
     CLI  <-->  |   Management API  |  127.0.0.1:9280
                |   Web Dashboard   |
                +---------+---------+
                          |
                          v
                +-------------------+
                |     Registry      |  SQLite (WAL)
                +---------+---------+
                          |
                          v
                +-------------------+       +------------------+
   Browser  ->  |   Reverse Proxy   |  ->   | Your dev server  |
                | 127.0.0.1:80/443  |       |   (localhost)    |
                +---------+---------+       +------------------+
                          |
                          v
                +-------------------+
                |   Local CA + Certs|  ~/.portflow/ca, ~/.portflow/certs
                +-------------------+
```

Daha ayrıntılı: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

---

## Tech Stack

- **Language:** Go 1.22+
- **CLI:** [`spf13/cobra`](https://github.com/spf13/cobra)
- **Database:** [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite)
  (pure Go, CGO gerektirmez)
- **Process discovery:** [`shirou/gopsutil`](https://github.com/shirou/gopsutil)
- **Docker discovery:** Docker Engine HTTP API over local unix socket (dependency-free)
- **Config:** [`gopkg.in/yaml.v3`](https://pkg.go.dev/gopkg.in/yaml.v3)
- **Desktop shell:** [Tauri 2](https://tauri.app)

---

## Development

```bash
git clone https://github.com/Mhuseyin7/PortFlow
cd PortFlow
go mod tidy
go build -o portflow .
go test ./...
```

Contribution guide: [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md).

---

## Status

**v0.1.0 — early spike.** Core path (registry → proxy → TLS → hosts)
çalışıyor. System service installer'lar (systemd / launchd / Windows
Service) ve TCP alias support v0.2 için planlı. Bug report ve feedback
çok değerli.

Changelog: [docs/CHANGELOG.md](docs/CHANGELOG.md).

---

## License

[MIT](LICENSE)

---

## Credits

PortFlow, **[Muhammed Koca](https://muhammedkoca.com.tr)** tarafından
open source olarak geliştirilmiştir.

- Web: <https://muhammedkoca.com.tr>
- GitHub: [@Mhuseyin7](https://github.com/Mhuseyin7)

Katkıda bulunanlara teşekkürler. Issue açmaktan ve PR göndermekten
çekinmeyin.
