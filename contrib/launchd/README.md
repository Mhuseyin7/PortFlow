# launchd service for PortFlow (macOS)

System-level daemon that binds :80 and :443 across all users. Needs root
to install, and the daemon runs as root — this is required to bind
privileged ports on macOS. The CA and leaf cert directories are still
per-user at `~/.portflow`.

```bash
# Install the binary
sudo install -m 0755 ./portflow /usr/local/bin/portflow

# Install the plist
sudo cp contrib/launchd/dev.portflow.daemon.plist /Library/LaunchDaemons/
sudo chown root:wheel /Library/LaunchDaemons/dev.portflow.daemon.plist
sudo launchctl bootstrap system /Library/LaunchDaemons/dev.portflow.daemon.plist

# Tail logs
tail -f /tmp/portflow.err.log

# Unload
sudo launchctl bootout system/dev.portflow.daemon
```

To run per-user instead (unprivileged, but ports 80/443 unavailable),
put the plist in `~/Library/LaunchAgents/` and use
`launchctl bootstrap gui/$UID`.
