// Package ide finds the code editors installed on this machine and opens a
// repository in one of them.
//
// It exists because "open in editor" used to mean exactly one thing —
// `exec.Command("code", path)` — which does nothing at all for anyone who does
// not have VS Code's CLI on their PATH, and fails silently when it is missing.
// Discovery here is deliberately conservative: a known set of editors rather
// than every application on the machine, so the picker is a short list of things
// the user would actually recognise.
package ide

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// IDE is one editor that can open a directory.
type IDE struct {
	Name string // "IntelliJ IDEA", "Visual Studio Code"
	// Bundle is a macOS .app path; Command is an executable on PATH. Exactly one
	// is set. A bundle is preferred when an editor offers both, because it works
	// whether or not the user ever installed the shell command.
	Bundle  string
	Command string
}

// ID is the stable handle stored in the configuration. It survives across runs
// and is what Lookup resolves.
func (i IDE) ID() string {
	if i.Bundle != "" {
		return i.Bundle
	}
	return "cmd:" + i.Command
}

// Exists reports whether the editor is still installed.
//
// This is checked every time the picker or the row control is built, not once at
// startup: applications get deleted, renamed and moved between launches, and a
// remembered editor that has quietly vanished should say so rather than fail on
// click.
func (i IDE) Exists() bool {
	if i.Bundle != "" {
		info, err := os.Stat(i.Bundle)
		return err == nil && info.IsDir()
	}
	if i.Command == "" {
		return false
	}
	_, err := exec.LookPath(i.Command)
	return err == nil
}

// Command builds the argv that opens dir. Nothing goes through a shell, so a
// repository path containing spaces or quotes is a plain argument.
func (i IDE) Args(dir string) ([]string, error) {
	switch {
	case i.Bundle != "":
		return []string{"open", "-a", i.Bundle, dir}, nil
	case i.Command != "":
		return []string{i.Command, dir}, nil
	default:
		return nil, fmt.Errorf("no editor configured")
	}
}

// knownBundles are the macOS applications recognised as editors, by the name
// their .app carries. Matching by name rather than listing every installed
// application keeps the picker to things the user would call an IDE.
//
// The prefix entries cover the families whose names carry an edition or version
// suffix — "IntelliJ IDEA Ultimate", "PyCharm Community Edition".
var knownBundles = []string{
	"IntelliJ IDEA", "PhpStorm", "WebStorm", "GoLand", "PyCharm", "RubyMine",
	"CLion", "DataGrip", "Rider", "RustRover", "Aqua", "Fleet", "Android Studio",
	"Visual Studio Code", "VSCodium", "Cursor", "Windsurf", "Zed", "Sublime Text",
	"Nova", "BBEdit", "Xcode", "Positron", "Trae", "Antigravity",
}

// knownCommands are editor launchers looked for on PATH. Terminal editors are
// deliberately absent: launched from a menu-bar app they would have no terminal
// to draw in.
var knownCommands = []struct{ name, command string }{
	{"Visual Studio Code", "code"},
	{"VS Code Insiders", "code-insiders"},
	{"VSCodium", "codium"},
	{"Cursor", "cursor"},
	{"Windsurf", "windsurf"},
	{"Zed", "zed"},
	{"Sublime Text", "subl"},
	{"IntelliJ IDEA", "idea"},
	{"PhpStorm", "phpstorm"},
	{"WebStorm", "webstorm"},
	{"GoLand", "goland"},
	{"PyCharm", "pycharm"},
	{"RubyMine", "rubymine"},
	{"CLion", "clion"},
	{"DataGrip", "datagrip"},
	{"Rider", "rider"},
}

// appDirs are the directories scanned for .app bundles. JetBrains Toolbox keeps
// its installs under the user's own Applications folder, which is why that is
// searched as well as the system one.
func appDirs() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	dirs := []string{"/Applications", "/Applications/Utilities"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, "Applications"),
			filepath.Join(home, "Applications", "JetBrains Toolbox"),
		)
	}
	return dirs
}

// Discover lists the editors installed on this machine, plus any the user added
// by hand. Results are sorted by name, and duplicates — an editor present both
// as a bundle and as a shell command — are collapsed to the bundle.
func Discover(extra []string) []IDE {
	found := map[string]IDE{} // keyed by display name

	for _, dir := range appDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a missing ~/Applications is normal, not an error
		}
		for _, e := range entries {
			name := strings.TrimSuffix(e.Name(), ".app")
			if name == e.Name() {
				continue // not a bundle
			}
			if !isKnownEditor(name) {
				continue
			}
			found[name] = IDE{Name: name, Bundle: filepath.Join(dir, e.Name())}
		}
	}

	for _, kc := range knownCommands {
		if _, ok := found[kc.name]; ok {
			continue // the bundle is the better handle
		}
		if _, err := exec.LookPath(kc.command); err == nil {
			found[kc.name] = IDE{Name: kc.name, Command: kc.command}
		}
	}

	// Anything the user pointed at explicitly is included whatever its name.
	for _, path := range extra {
		if i, ok := FromPath(path); ok {
			found[i.Name] = i
		}
	}

	out := make([]IDE, 0, len(found))
	for _, i := range found {
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name) })
	return out
}

// isKnownEditor matches an application name against the known set, allowing the
// edition and version suffixes the JetBrains family carries.
func isKnownEditor(name string) bool {
	for _, known := range knownBundles {
		if name == known || strings.HasPrefix(name, known+" ") {
			return true
		}
	}
	return false
}

// FromPath builds an IDE from a path the user chose by hand — a .app bundle or
// any executable.
func FromPath(path string) (IDE, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return IDE{}, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return IDE{}, false
	}
	if strings.HasSuffix(path, ".app") && info.IsDir() {
		return IDE{Name: strings.TrimSuffix(filepath.Base(path), ".app"), Bundle: path}, true
	}
	if info.IsDir() {
		return IDE{}, false
	}
	return IDE{Name: filepath.Base(path), Command: path}, true
}

// Lookup resolves a stored ID back to an editor. found reports whether the ID
// was recognised at all; the caller still has to ask Exists, because an editor
// that has since been deleted must be shown as missing rather than silently
// replaced by another.
func Lookup(id string, extra []string) (IDE, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return IDE{}, false
	}
	if strings.HasPrefix(id, "cmd:") {
		cmd := strings.TrimPrefix(id, "cmd:")
		name := cmd
		for _, kc := range knownCommands {
			if kc.command == cmd {
				name = kc.name
				break
			}
		}
		return IDE{Name: name, Command: cmd}, true
	}
	// A bundle path. Its name is derived rather than looked up, so an editor that
	// is no longer installed still has something to display next to "not found".
	return IDE{Name: strings.TrimSuffix(filepath.Base(id), ".app"), Bundle: id}, true
}
