# Contributing

Thanks for helping PortFlow.

## Development setup

Requirements:

- Go 1.22+
- On Linux/macOS, a shell that can sudo (needed for the hosts file and
  trust store tests).
- On Windows, an Administrator PowerShell for the same reason.

```bash
git clone https://github.com/portflow/portflow
cd portflow
go build -o portflow .
go test ./...
```

The daemon needs elevation to bind ports 80/443 and write the hosts
file. During development you can avoid that with:

```bash
./portflow daemon start --http 127.0.0.1:8080 --https 127.0.0.1:8443
```

…and turn hosts management off in `~/.portflow/config.yml` (planned).

## Layout

See [ARCHITECTURE.md](ARCHITECTURE.md).

## Style

- `gofmt` / `goimports` clean.
- Public functions get a short doc comment. Internal helpers do not,
  unless the *why* is non-obvious.
- Prefer table-driven tests for validation & parsing.
- No dependencies added without discussion. PortFlow's promise is that
  it's small.

## Commit messages

Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
`chore:`).

## Adding a platform adapter

For a new OS trust store or hosts backend, add a new build-tagged file
in `internal/trust/` or `internal/hosts/`. Keep the interface identical
to the existing platforms — the daemon should not have to know which
OS it's on.

## Releases

Tag `vX.Y.Z`; GitHub Actions builds artifacts for
`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`,
`windows/amd64`.
