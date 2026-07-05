# Developer FAQ

Common development gotchas and the shortest correct answer for each.

## GNOME extension

### Why does `task gnome:restart` update the installed files, but GNOME still shows old behavior?

GNOME Shell can keep the old extension code loaded in memory even after the files on disk were replaced.

Use:

```sh
gnome-extensions disable git-repo-tracker@laszukdawid.github.com
gnome-extensions enable git-repo-tracker@laszukdawid.github.com
```

If the running Shell still keeps stale code, log out/in. On Wayland that is the reliable full reload path.

### Is nested GNOME Shell enough to test the extension?

Only for the Shell UI itself.

Nested GNOME is good for:

- panel menu layout
- CSS
- submenu behavior
- click handlers that stay inside GNOME Shell

Nested GNOME is not reliable for:

- windows opened by external GUI processes
- the Fyne Settings window launched from the extension

Reason: the extension runs inside the nested Shell, but the Settings action launches a separate `git-repo-tracker --settings` process. In nested sessions that new GUI window may open on the outer session, fail to map visibly, or otherwise not appear where you expect.

### How should I test the GNOME Settings button, then?

Split the workflow:

1. Use nested GNOME Shell to test extension menu/layout changes.
2. Use the real desktop session to test the Settings button and the Fyne window.

Direct Fyne settings smoke test:

```sh
~/.local/bin/git-repo-tracker --settings
```

Real-session extension reload without rebooting:

```sh
gnome-extensions disable git-repo-tracker@laszukdawid.github.com
gnome-extensions enable git-repo-tracker@laszukdawid.github.com
```

### Which binaries does the GNOME extension use?

By default it looks for:

```text
~/.local/bin/git-repo-tracker-cli
~/.local/bin/git-repo-tracker
```

`task gnome:install` should rebuild/install those for development.

Override them with:

```text
GIT_REPO_TRACKER_CLI=/absolute/path/to/git-repo-tracker-cli
GIT_REPO_TRACKER_APP=/absolute/path/to/git-repo-tracker
```

Those environment variables must be visible to GNOME Shell's session.

### Why did the custom GNOME panel SVG icon not show up, even after reinstall/restart?

Do not assume a bundled file-backed SVG will render reliably in the GNOME top bar just because the extension code loaded.

What failed here:

- the extension code was refreshing correctly
- the panel slot existed
- the custom SVG icon still rendered as nothing

For GNOME panel work, prefer the simplest visible primitive first:

- a Shell-rendered glyph in `St.Label`
- or a stock symbolic icon

Only move to a custom file-backed icon if there is a strong reason and it is visually verified in GNOME Shell.

Practical rule: if a custom panel icon is invisible, switch to a text/glyph-based mark before debugging more complex asset-loading paths.

## Fyne UI

### Why do custom-painted surfaces and stock Fyne widgets sometimes look like mixed themes?

Because this app uses both:

- Fyne theme colors for stock widgets
- custom palette colors baked into canvas objects

When changing appearance behavior, keep those two paths synchronized. If the app follows system theme changes, both the Fyne theme and the custom palette must resolve to the same concrete light/dark variant.
