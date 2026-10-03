# systemd user service for PortFlow

```bash
# Install the binary
sudo install -m 0755 ./portflow /usr/local/bin/portflow

# Copy the unit file (replace %i template with your username, or use
# the templated form below)
sudo cp contrib/systemd/portflow.service /etc/systemd/system/portflow@.service
sudo systemctl daemon-reload

# Enable + start for your user
sudo systemctl enable --now portflow@$USER

# Check it
systemctl status portflow@$USER
journalctl -u portflow@$USER -f
```

The unit grants `CAP_NET_BIND_SERVICE` so the daemon can bind to :80 and
:443 without being root. It sandboxes the process with `ProtectSystem`,
`ProtectHome`, `PrivateTmp` and `NoNewPrivileges`, and whitelists
`~/.portflow` and `/etc/hosts` as writable paths.
