# GNOME Shell Extension

This is the optional Ubuntu/Fedora GNOME frontend. It is separate from the native
Linux Fyne/AppIndicator app: the native app keeps the tray menu compact and opens
the full Fyne UI, while this extension renders a GNOME-native panel menu using
Shell widgets.

The extension reads repo state from the headless Go CLI, so config, discovery,
refresh behavior, and cache state still come from the shared Go backend.

Development and install instructions live in
[`docs/GNOME_EXTENSION.md`](../../docs/GNOME_EXTENSION.md).
