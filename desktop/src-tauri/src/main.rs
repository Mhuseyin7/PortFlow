// PortFlow desktop shell — a thin Tauri wrapper around the daemon's
// built-in dashboard at http://127.0.0.1:9280.
//
// Keeping the UI in the Go daemon (not here) means the CLI, the browser
// and the desktop app all see identical behavior; this file just gives
// the user a native window.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    tauri::Builder::default()
        .setup(|_app| Ok(()))
        .run(tauri::generate_context!())
        .expect("error while running PortFlow desktop");
}
