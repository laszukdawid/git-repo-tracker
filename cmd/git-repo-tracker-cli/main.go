// Command git-repo-tracker-cli exposes the repo tracker backend without a GUI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/laszukdawid/git-repo-tracker/internal/backend"
	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
	"github.com/laszukdawid/git-repo-tracker/internal/ui/actions"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

type statusDoc struct {
	GeneratedAt time.Time           `json:"generatedAt"`
	Total       int                 `json:"total"`
	Behind      int                 `json:"behind"`
	Repos       []monitor.RepoState `json:"repos"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "status":
		status(os.Args[2:])
	case "details":
		details(os.Args[2:])
	case "open":
		openRepo(os.Args[2:])
	case "pull":
		pull(os.Args[2:])
	case "update-all":
		updateAll(os.Args[2:])
	case "-v", "--version", "version":
		fmt.Printf("git-repo-tracker-cli %s\n", version)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "git-repo-tracker-cli: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func pull(args []string) {
	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "git-repo-tracker-cli: pull requires a repository path")
		os.Exit(2)
	}
	if err := loadService().Pull(fs.Arg(0)); err != nil {
		fatal("pull %s: %v", fs.Arg(0), err)
	}
}

func details(args []string) {
	fs := flag.NewFlagSet("details", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "git-repo-tracker-cli: details requires a repository path")
		os.Exit(2)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(loadService().Details(fs.Arg(0))); err != nil {
		fatal("encode details: %v", err)
	}
}

func openRepo(args []string) {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "git-repo-tracker-cli: open requires a repository path")
		os.Exit(2)
	}
	svc := loadService()
	action, custom := svc.Config().Click()
	if err := actions.Run(action, custom, fs.Arg(0)); err != nil {
		fatal("open %s: %v", fs.Arg(0), err)
	}
}

func updateAll(args []string) {
	fs := flag.NewFlagSet("update-all", flag.ExitOnError)
	_ = fs.Parse(args)

	svc := loadService()
	svc.RefreshNow(false)
	var failed int
	for _, r := range svc.Snapshot() {
		if r.Behind <= 0 {
			continue
		}
		if err := svc.Pull(r.Path); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "pull %s: %v\n", r.Path, err)
		}
	}
	if failed > 0 {
		os.Exit(1)
	}
}

func status(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOut := fs.Bool("json", true, "print JSON output")
	refresh := fs.Bool("refresh", false, "scan repos and refresh local status before printing")
	fetch := fs.Bool("fetch", false, "also fetch remotes during --refresh")
	_ = fs.Parse(args)

	svc := loadService()
	if *refresh || *fetch {
		svc.RefreshNow(*fetch)
	}
	total, behind := svc.Counts()
	doc := statusDoc{
		GeneratedAt: time.Now(),
		Total:       total,
		Behind:      behind,
		Repos:       svc.Snapshot(),
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(doc); err != nil {
			fatal("encode status: %v", err)
		}
		return
	}
	fmt.Printf("%d repos · %d behind\n", doc.Total, doc.Behind)
	for _, r := range doc.Repos {
		fmt.Printf("%s\t%s\tbehind=%d\tahead=%d\tdirty=%v\t%s\n", r.Name, r.Branch, r.Behind, r.Ahead, r.Dirty, r.Path)
	}
}

func loadService() *backend.Service {
	cfg, err := backend.LoadDefaultConfig()
	if err != nil {
		fatal("%v", err)
	}
	return backend.New(cfg, nil, nil)
}

func usage() {
	fmt.Printf(`git-repo-tracker-cli %s - headless repo tracker backend

Usage:
  git-repo-tracker-cli status [--json] [--refresh] [--fetch]
  git-repo-tracker-cli details <repo-path>
  git-repo-tracker-cli open <repo-path>
  git-repo-tracker-cli pull <repo-path>
  git-repo-tracker-cli update-all
  git-repo-tracker-cli --version
  git-repo-tracker-cli --help

Commands:
  status     Print the current cached repo snapshot. Use --refresh to rescan and
             recompute local status. Add --fetch to refresh remotes too.
  details    Print one repository's path and commit details as JSON.
  open       Run the configured clickAction for one repository.
  pull       Fast-forward one repository.
  update-all Fast-forward every repository that is behind origin.
`, version)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "git-repo-tracker-cli: "+format+"\n", args...)
	os.Exit(1)
}
