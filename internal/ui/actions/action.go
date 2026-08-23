package actions

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
	"github.com/laszukdawid/git-repo-tracker/internal/git"
)

// Run performs the configured click action for a repository directory. The
// process gets a PATH augmented to match a desktop session (via git.Env), so
// helpers like `open`, `code` or `xdg-open` resolve even when the app was started
// from Finder or a login item.
func Run(action, custom, path string) error {
	cmd, err := Command(action, custom, path)
	if err != nil {
		return err
	}
	cmd.Env = git.Env()
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap the child once it exits; without Wait a long-running tray process
	// accumulates zombie entries for every launched helper.
	go func() { _ = cmd.Wait() }()
	return nil
}

// RunArgs launches an already-built argv. It shares Run's environment handling —
// the augmented PATH a desktop session would have — and its reaping, so callers
// that assemble their own command (opening a repository in a chosen editor, say)
// do not each reinvent both.
func RunArgs(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("nothing to run")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = git.Env()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func Command(action, custom, path string) (*exec.Cmd, error) {
	switch action {
	case config.ActionTerminal:
		return terminalCommand(path), nil
	case config.ActionEditor:
		// Superseded by the row's editor chip, which asks what is actually
		// installed. This stays for configurations that still name it, and now
		// resolves rather than assuming VS Code's CLI is on PATH — which is what
		// made the old hard-coded `code` fail silently for everyone without it.
		args, _ := ResolveEditorCommand(path)
		return exec.Command(args[0], args[1:]...), nil
	case config.ActionCustom:
		args := splitArgs(custom)
		if len(args) == 0 {
			return nil, fmt.Errorf("custom command is empty")
		}
		for i := range args {
			args[i] = strings.ReplaceAll(args[i], "{path}", path)
		}
		if !strings.Contains(custom, "{path}") {
			args = append(args, path)
		}
		return exec.Command(args[0], args[1:]...), nil
	default: // ActionOpenFolder
		return fileManagerCommand(path), nil
	}
}

// ResolveEditorCommand returns the exact argv used by the legacy editor click
// action. fallback reports that resolution failed and Command will retain its
// historical `code` fallback.
func ResolveEditorCommand(dir string) (args []string, fallback bool) {
	if args, err := editorArgs(dir); err == nil {
		return args, false
	}
	return []string{"code", dir}, true
}

// editorArgs resolves the configured editor, if one has been chosen, into the
// command that opens dir in it. resolveEditor is installed by the ui package,
// which owns the setting; without it this falls back to the old behaviour.
var resolveEditor func(dir string) ([]string, error)

// SetEditorResolver installs the resolver used by the "editor" click action.
func SetEditorResolver(fn func(dir string) ([]string, error)) { resolveEditor = fn }

func editorArgs(dir string) ([]string, error) {
	if resolveEditor == nil {
		return nil, fmt.Errorf("no editor resolver installed")
	}
	return resolveEditor(dir)
}

// fileManagerCommand reveals a directory in the OS file manager.
func fileManagerCommand(path string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path)
	case "windows":
		return exec.Command("explorer", path)
	default:
		return exec.Command("xdg-open", path)
	}
}

// terminalCommand opens a terminal in the directory.
func terminalCommand(path string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-a", "Terminal", path)
	case "windows":
		// Open a new console with its working directory set rather than building a
		// shell string that would re-interpret special characters in the path.
		c := exec.Command("cmd", "/c", "start", "cmd")
		c.Dir = path
		return c
	default:
		// x-terminal-emulator is the Debian alternatives name most distros provide.
		return exec.Command("x-terminal-emulator", "--working-directory="+path)
	}
}

// splitArgs splits a command string into argv, honouring single and double
// quotes so templates like `open -a "Visual Studio Code" {path}` group correctly.
// There is no shell involved (we exec the argv directly), so this is purely about
// argument grouping, not interpretation — no injection surface. An unterminated
// quote is treated as closed at end of string.
func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inArg := false
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			inArg = true
		case r == '\'' || r == '"':
			quote = r
			inArg = true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}
