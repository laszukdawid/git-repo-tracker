package git

import (
	"strings"
	"testing"
	"time"
)

// nul builds a for-each-ref line from its fields.
func nul(fields ...string) string { return strings.Join(fields, "\x00") }

func TestParseLocalBranches(t *testing.T) {
	out := []byte(strings.Join([]string{
		nul("main", "aaa111", "refs/remotes/origin/main", "", "1755800000",
			"2026-08-21T20:13:20+02:00", "/Users/me/work", "*", "fix: the thing, properly"),
		nul("feature/x", "bbb222", "refs/remotes/origin/feature/x", "ahead 1, behind 2",
			"1755700000", "2026-08-20T16:26:40+02:00", "", " ", "wip"),
		nul("orphan", "ccc333", "", "", "1755600000", "2026-08-19T12:40:00+02:00", "", " ", ""),
		nul("stale", "ddd444", "refs/remotes/origin/stale", "gone", "1755500000",
			"2026-08-18T08:53:20+02:00", "", " ", "old work"),
		"malformed line without separators",
	}, "\n"))

	bs := parseLocalBranches(out)
	if len(bs) != 4 {
		t.Fatalf("parsed %d branches, want 4 (the malformed line must be skipped)", len(bs))
	}

	main := bs[0]
	if !main.Current {
		t.Error("main should be current — %(HEAD) was \"*\"")
	}
	if main.WorktreePath != "/Users/me/work" {
		t.Errorf("worktree = %q", main.WorktreePath)
	}
	if main.UpstreamName != "origin/main" {
		t.Errorf("upstream display = %q, want origin/main", main.UpstreamName)
	}
	if main.Subject != "fix: the thing, properly" {
		t.Errorf("a subject with a comma must survive: %q", main.Subject)
	}
	if !main.CommitTime.Equal(time.Unix(1755800000, 0)) {
		t.Errorf("commit time = %v", main.CommitTime)
	}

	// %(HEAD) is a single space for a non-current branch, not empty.
	if bs[1].Current {
		t.Error("feature/x should not be current")
	}
	if bs[1].Ahead != 1 || bs[1].Behind != 2 {
		t.Errorf("track = %d ahead / %d behind, want 1/2", bs[1].Ahead, bs[1].Behind)
	}

	if bs[2].HasUpstream() {
		t.Error("orphan has no upstream")
	}
	if !bs[3].Gone {
		t.Error("stale's upstream is gone")
	}
	if bs[3].Updatable() {
		t.Error("a branch whose upstream is gone cannot be updated")
	}
}

func TestParseWorktreesPorcelain(t *testing.T) {
	out := []byte(strings.Join([]string{
		"worktree /work/main", "HEAD aaa111", "branch refs/heads/main", "",
		"worktree /work/feature with spaces", "HEAD bbb222", "branch refs/heads/feature/x", "locked IDE session", "",
		"worktree /work/detached", "HEAD ccc333", "detached", "",
		"worktree /work/stale", "HEAD ddd444", "branch refs/heads/old", "prunable gitdir file points to non-existent location", "",
	}, "\x00"))

	worktrees := parseWorktrees(out)
	if len(worktrees) != 4 {
		t.Fatalf("parsed %d worktrees, want 4", len(worktrees))
	}
	if got := worktrees[0]; got.Path != "/work/main" || got.Branch != "main" || got.Head != "aaa111" {
		t.Errorf("main worktree = %+v", got)
	}
	if got := worktrees[1]; got.Path != "/work/feature with spaces" || got.Branch != "feature/x" || got.Locked != "IDE session" {
		t.Errorf("locked worktree = %+v", got)
	}
	if got := worktrees[2]; !got.Detached || got.Branch != "" || got.Head != "ccc333" {
		t.Errorf("detached worktree = %+v", got)
	}
	if got := worktrees[3]; got.Prunable != "gitdir file points to non-existent location" {
		t.Errorf("prunable worktree = %+v", got)
	}
}

func TestParseTrack(t *testing.T) {
	cases := []struct {
		in            string
		ahead, behind int
		gone          bool
	}{
		{"", 0, 0, false},
		{"gone", 0, 0, true},
		{"ahead 3", 3, 0, false},
		{"behind 2", 0, 2, false},
		{"ahead 1, behind 2", 1, 2, false},
		{"nonsense", 0, 0, false},
	}
	for _, tc := range cases {
		a, b, g := parseTrack(tc.in)
		if a != tc.ahead || b != tc.behind || g != tc.gone {
			t.Errorf("parseTrack(%q) = %d,%d,%v; want %d,%d,%v", tc.in, a, b, g, tc.ahead, tc.behind, tc.gone)
		}
	}
}

func TestParseRemoteBranches(t *testing.T) {
	// The %(if)%(symref) guard skips origin/HEAD but still emits a blank line.
	out := []byte(strings.Join([]string{
		"",
		nul("spike", "eee555", "1755400000", "try something"),
		nul("nested/thing", "fff666", "1755300000", ""),
	}, "\n"))

	bs := parseRemoteBranches(out, "origin")
	if len(bs) != 2 {
		t.Fatalf("parsed %d, want 2 — the blank origin/HEAD line must be skipped", len(bs))
	}
	if bs[0].Kind != BranchRemoteOnly {
		t.Error("remote branches must be marked remote-only")
	}
	if bs[0].RemoteRef != "refs/remotes/origin/spike" {
		t.Errorf("remote ref = %q", bs[0].RemoteRef)
	}
	if bs[1].Name != "nested/thing" {
		t.Errorf("nested name = %q — lstrip=3 should keep the slash", bs[1].Name)
	}

	if got := parseRemoteBranches(nil, "origin"); len(got) != 0 {
		t.Errorf("no remote should give no branches, got %d", len(got))
	}
}

func TestMergeAndSortBranches(t *testing.T) {
	local := []Branch{
		{Name: "main", Current: true, CommitTime: time.Unix(1000, 0)},
		{Name: "feature", CommitTime: time.Unix(3000, 0)},
		{Name: "older-local", CommitTime: time.Unix(500, 0)},
	}
	remote := []Branch{
		{Name: "main", Kind: BranchRemoteOnly, CommitTime: time.Unix(1000, 0)},
		{Name: "spike", Kind: BranchRemoteOnly, CommitTime: time.Unix(9000, 0)},
	}

	all := mergeBranches(local, remote)
	if len(all) != 4 {
		t.Fatalf("merged to %d, want 4 — the remote main duplicates a local branch", len(all))
	}
	sortBranches(all)

	// Current first even though it is the oldest; then the branches you actually
	// have locally, newest first; and only then the remote-only ones — "spike" is
	// the most recent of all and still sorts last, which is the point.
	want := []string{"main", "feature", "older-local", "spike"}
	for i, name := range want {
		if all[i].Name != name {
			t.Errorf("position %d = %q, want %q", i, all[i].Name, name)
		}
	}
}

func TestClassifyBranchError(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   BranchErrKind
	}{
		{"diverged fetch", " ! [rejected]        origin/x -> x  (non-fast-forward)", BranchErrDiverged},
		{"diverged merge", "fatal: Not possible to fast-forward, aborting.", BranchErrDiverged},
		{"checked out", "fatal: refusing to fetch into branch 'refs/heads/x' checked out at '/Users/me/wt'", BranchErrCheckedOut},
		{"dirty", "error: Your local changes to the following files would be overwritten by merge:\n\tmain.go\nAborting", BranchErrDirtyTree},
		{"untracked", "error: The following untracked working tree files would be overwritten by merge:\n\tnew.go\nAborting", BranchErrUntracked},
		{"locked", "error: cannot lock ref 'refs/heads/x': Unable to create '.git/refs/heads/x.lock'", BranchErrLocked},
		{"gone", "fatal: couldn't find remote ref refs/remotes/origin/x", BranchErrGone},
		{"exists", "fatal: a branch named 'x' already exists", BranchErrExists},
		{"unknown", "fatal: something else entirely", BranchErrUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			be := classifyBranchError("x", 1, tc.stderr)
			if be.Kind != tc.want {
				t.Errorf("kind = %v, want %v", be.Kind, tc.want)
			}
			if be.Message() == "" {
				t.Error("every classification must produce a message")
			}
		})
	}

	// The worktree path is extracted so the message can name it.
	be := classifyBranchError("x", 128,
		"fatal: refusing to fetch into branch 'refs/heads/x' checked out at '/Users/me/wt'")
	if be.Detail != "/Users/me/wt" {
		t.Errorf("worktree detail = %q", be.Detail)
	}
	if !strings.Contains(be.Message(), "/Users/me/wt") {
		t.Errorf("message should name the worktree: %q", be.Message())
	}
}

// The dirty-tree refusals are exactly the case lastLine gets wrong: git's last
// line there is the bare word "Aborting".
func TestFirstErrorLineBeatsLastLineForMergeRefusals(t *testing.T) {
	stderr := "error: Your local changes to the following files would be overwritten by merge:\n" +
		"\tinternal/ui/app.go\nPlease commit your changes or stash them before you merge.\nAborting"
	if got := lastLine(stderr); got != "Aborting" {
		t.Fatalf("precondition changed: lastLine = %q", got)
	}
	got := firstErrorLine(stderr)
	if !strings.Contains(got, "local changes") {
		t.Errorf("firstErrorLine = %q, want the reason git states first", got)
	}
}

func TestFirstErrorLine(t *testing.T) {
	cases := map[string]string{
		"":                            "",
		"fatal: boom":                 "boom",
		"error: nope":                 "nope",
		"hint: try this\nfatal: real": "real",
		"warning: meh\nremote: noise\nfatal: real": "real",
		"plain line": "plain line",
	}
	for in, want := range cases {
		if got := firstErrorLine(in); got != want {
			t.Errorf("firstErrorLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// The error string a failed git command produces must not change: it is stored
// in the state cache and shown in the UI, and the typed error was introduced
// underneath it purely to make classification possible.
func TestCmdErrorMessageUnchanged(t *testing.T) {
	e := &cmdError{
		Args:     []string{"fetch", "--quiet"},
		ExitCode: 128,
		Stderr:   "hint: something\nfatal: could not read from remote repository\n",
	}
	want := "git fetch --quiet: fatal: could not read from remote repository"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// Within your own branches, the ones with something to pull come first: in a
// repository with two hundred branches the handful that are actually behind are
// otherwise scattered through a list nobody scrolls to the end of.
func TestSortBranchesPutsBehindBranchesFirst(t *testing.T) {
	now := time.Now()
	bs := []Branch{
		{Name: "old-but-behind", CommitTime: now.Add(-90 * 24 * time.Hour), Behind: 4},
		{Name: "current", Current: true, CommitTime: now.Add(-time.Hour)},
		{Name: "someone-elses", Kind: BranchRemoteOnly, CommitTime: now, Behind: 9},
		{Name: "fresh-and-level", CommitTime: now.Add(-time.Minute)},
		{Name: "newer-and-behind", CommitTime: now.Add(-2 * time.Hour), Behind: 1},
	}
	sortBranches(bs)

	var got []string
	for _, b := range bs {
		got = append(got, b.Name)
	}
	want := []string{"current", "newer-and-behind", "old-but-behind", "fresh-and-level", "someone-elses"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// A refused push and a refused fast-forward both print "! [rejected]", and they
// call for opposite actions. Classifying one as the other tells the user to do
// exactly the wrong thing.
func TestClassifyDistinguishesRejectedPushFromRejectedFetch(t *testing.T) {
	fetchRejected := " ! [rejected]        origin/topic -> topic  (non-fast-forward)\n"
	if got := classifyBranchError("topic", 1, fetchRejected); got.Kind != BranchErrDiverged {
		t.Errorf("a refused fast-forward classified as %d, want diverged", got.Kind)
	}

	pushRejected := " ! [rejected]        topic -> topic (fetch first)\n" +
		"error: failed to push some refs to 'origin'\n" +
		"hint: Updates were rejected because the remote contains work that you do not have locally.\n"
	if got := classifyBranchError("topic", 1, pushRejected); got.Kind != BranchErrPushRejected {
		t.Errorf("a refused push classified as %d, want push-rejected", got.Kind)
	}

	conflicted := "Auto-merging f.txt\nCONFLICT (content): Merge conflict in f.txt\n" +
		"Automatic merge failed; fix conflicts and then commit the result.\n"
	if got := classifyBranchError("topic", 1, conflicted); got.Kind != BranchErrConflicted {
		t.Errorf("a conflicted merge classified as %d, want conflicted", got.Kind)
	}
}
