# Privacy

PortFlow is local-first.

- No telemetry. No analytics. No crash reporter. No update pings.
- No network calls to any PortFlow-controlled server. There is no
  PortFlow-controlled server.
- All state is on your machine, in your home directory under
  `~/.portflow/`.
- The dashboard is served by the local daemon and never phones home.
- The reverse proxy does not persist request payloads. If you enable
  the optional request log, it lives on your machine, and the
  `Authorization`, `Cookie` and `Set-Cookie` headers are redacted.

Removing PortFlow completely:

```bash
portflow untrust
portflow daemon stop
rm -rf ~/.portflow
```

That's it. There is nothing to unsubscribe from.
