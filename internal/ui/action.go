package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/dawidlaszuk/git-repo-tracker/internal/config"
	"github.com/dawidlaszuk/git-repo-tracker/internal/git"
)

// runAction performs the configured click action for a repository directory. The
// process gets a PATH augmented to match a desktop session (via git.Env), so
// helpers like `open`, `code` or `xdg-open` resolve even when the app was started
// from Finder or a login item.
func runAction(action, custom, path string) error {
	cmd, err := actionCommand(action, custom, path)
	if err != nil {
		return err
	}
	cmd.Env = git.Env()
	return cmd.Start()
}

func actionCommand(action, custom, path string) (*exec.Cmd, error) {
	switch action {
	case config.ActionTerminal:
		return terminalCommand(path), nil
	case config.ActionEditor:
		return exec.Command("code", path), nil
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
		// Open a new console *with its working directory set* rather than building
		// a `cd /d <path>` shell string — that would let special characters in the
		// path inject extra commands.
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
