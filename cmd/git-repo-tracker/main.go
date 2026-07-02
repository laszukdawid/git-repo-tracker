// Command git-repo-tracker is a cross-platform system-tray app that watches the
// status of local git repositories. It discovers repos under configured
// directories, periodically checks how far each local branch is behind its
// origin, and surfaces the results in the tray plus a searchable browser window.
package main

import (
	"fmt"
	"os"

	"github.com/laszukdawid/git-repo-tracker/internal/backend"
	"github.com/laszukdawid/git-repo-tracker/internal/ui"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-v", "--version":
			fmt.Printf("git-repo-tracker %s\n", version)
			return
		case "-h", "--help":
			usage()
			return
		default:
			fmt.Fprintf(os.Stderr, "git-repo-tracker: unknown argument %q (try --help)\n", arg)
			os.Exit(2)
		}
	}

	cfg, err := backend.LoadDefaultConfig()
	if err != nil {
		fatal("%v", err)
	}

	app, err := ui.NewApp(cfg)
	if err != nil {
		fatal("%v", err)
	}
	app.Run()
}

func usage() {
	fmt.Printf(`git-repo-tracker %s — system-tray tracker for local git repositories.

Usage:
  git-repo-tracker            Launch the tray app.
  git-repo-tracker --version  Print the version and exit.
  git-repo-tracker --help     Show this help and exit.

Config:
  ~/Library/Application Support/git-repo-tracker/config.yaml   (macOS)
  ~/.config/git-repo-tracker/config.yaml                       (Linux)
  Override with GIT_REPO_TRACKER_CONFIG=/path/to/config.yaml
`, version)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "git-repo-tracker: "+format+"\n", args...)
	os.Exit(1)
}
