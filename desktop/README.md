# PortFlow desktop (Tauri)

A thin Tauri 2 wrapper around the daemon's built-in dashboard.

- The daemon serves the same HTML/CSS/JS at `http://127.0.0.1:9280/`.
- This shell simply points a system webview at that URL and gives you a
  native window with a menu bar and tray icon.
- Keeping the UI in one place means CLI users, browser users and
  desktop users all see identical behavior.

## Dev

```bash
cd desktop
npm install
npm run tauri dev
```

Requires the Rust toolchain and platform Tauri prerequisites (see
https://tauri.app/start/prerequisites/). The dev daemon must be running:

```bash
../portflow daemon start
```

## Build

```bash
npm run tauri build
```

Artifacts land under `src-tauri/target/release/bundle/`.
