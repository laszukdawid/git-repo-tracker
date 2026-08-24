// Package loginitem manages launching the app automatically at login. On macOS
// it writes a LaunchAgent plist into ~/Library/LaunchAgents (launchd loads those
// at login, so no privileged calls are needed). On Linux it writes an XDG
// autostart .desktop file. Toggling is just creating or removing that file.
package loginitem

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Label is the reverse-DNS identifier and file stem used for the login item.
const Label = "com.github.laszukdawid.git-repo-tracker"

// appName is the human-readable name used in the Linux .desktop entry.
const appName = "git-repo-tracker"

// Enabled reports whether the login item is currently installed.
func Enabled() bool {
	p, err := itemPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Sync reconciles the on-disk login item with the desired state. Changes take
// effect at the next login (we deliberately do not start a second instance in
// the current session).
func Sync(want bool) error {
	switch runtime.GOOS {
	case "darwin", "linux":
		if want {
			return enable()
		}
		return disable()
	default:
		if want {
			return fmt.Errorf("launch at login is not supported on %s", runtime.GOOS)
		}
		return nil
	}
}

// itemPath returns the per-OS path of the login-item file.
func itemPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
	case "linux":
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "autostart", appName+".desktop"), nil
	default:
		return "", fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

func enable() error {
	p, err := itemPath()
	if err != nil {
		return err
	}
	args, err := programArguments()
	if err != nil {
		return err
	}
	var content string
	if runtime.GOOS == "darwin" {
		content = renderPlist(args)
	} else {
		content = renderDesktop(args)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func disable() error {
	p, err := itemPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// programArguments returns the command used to launch the app. On macOS, when
// launched from a .app bundle we go through `open` so LaunchServices activates
// the bundle properly; otherwise we point straight at the executable.
func programArguments() ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if runtime.GOOS == "darwin" {
		if bundle, ok := appBundle(exe); ok {
			return []string{"/usr/bin/open", bundle}, nil
		}
	}
	return []string{exe}, nil
}

// appBundle returns the enclosing .app path if exe lives inside a macOS bundle.
func appBundle(exe string) (string, bool) {
	const marker = ".app/Contents/MacOS/"
	if i := strings.Index(exe, marker); i != -1 {
		return exe[:i+len(".app")], true
	}
	return "", false
}

func renderPlist(args []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")
	b.WriteString("\t<key>Label</key>\n\t<string>" + xmlEscape(Label) + "</string>\n")
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range args {
		b.WriteString("\t\t<string>" + xmlEscape(a) + "</string>\n")
	}
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	// Interactive: this is a user-facing GUI app, not a background daemon.
	b.WriteString("\t<key>ProcessType</key>\n\t<string>Interactive</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// renderDesktop builds an XDG autostart entry. Exec needs a single command
// string; every argument is quoted per the Desktop Entry spec so a path with
// spaces, quotes, `$`, backslashes or `%` field codes round-trips verbatim.
func renderDesktop(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quoteExecArg(a)
	}
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Name=" + appName + "\n")
	b.WriteString("Exec=" + strings.Join(quoted, " ") + "\n")
	b.WriteString("Terminal=false\n")
	b.WriteString("X-GNOME-Autostart-enabled=true\n")
	return b.String()
}

// quoteExecArg quotes one Exec= argument. The Desktop Entry spec reserves `%`
// as a field-code prefix (escaped as `%%`, applied even outside quotes) and,
// inside double quotes, requires `"`, “ ` “, `$` and `\` to be backslash-escaped.
// Plain alphanumeric/path arguments are left bare for readability.
func quoteExecArg(a string) string {
	a = strings.ReplaceAll(a, "%", "%%")
	if !strings.ContainsAny(a, " \t\n\"'`$\\<>~|&;*?#()") {
		return a
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return `"` + r.Replace(a) + `"`
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
