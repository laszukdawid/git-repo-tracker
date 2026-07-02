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
  git-repo-tracker-cli --version
  git-repo-tracker-cli --help

Commands:
  status     Print the current cached repo snapshot. Use --refresh to rescan and
             recompute local status. Add --fetch to refresh remotes too.
`, version)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "git-repo-tracker-cli: "+format+"\n", args...)
	os.Exit(1)
}
