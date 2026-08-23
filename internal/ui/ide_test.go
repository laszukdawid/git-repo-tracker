package ui

import (
	"strings"
	"testing"

	"github.com/laszukdawid/git-repo-tracker/internal/ide"
)

// The wording is the whole of the control's explanation — the chip itself is
// just an icon — so each state has to say something different and useful.
func TestIDETooltips(t *testing.T) {
	cases := []struct {
		name       string
		choice     ideChoice
		wantOpen   string
		wantChange string
	}{
		{
			name:       "nothing chosen yet",
			choice:     ideChoice{},
			wantOpen:   "Select an editor",
			wantChange: "Select an editor",
		},
		{
			name:       "chosen and installed",
			choice:     ideChoice{set: true, editor: ide.IDE{Name: "PhpStorm", Bundle: "/Applications/PhpStorm.app"}},
			wantOpen:   "Open with PhpStorm",
			wantChange: "different editor",
		},
		{
			name:       "chosen but deleted since",
			choice:     ideChoice{set: true, missing: true, editor: ide.IDE{Name: "Cursor", Bundle: "/Applications/Cursor.app"}},
			wantOpen:   "(not found)",
			wantChange: "Choose another",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			open, change := ideTooltips(tc.choice)
			if !strings.Contains(open, tc.wantOpen) {
				t.Errorf("open tip = %q, want it to mention %q", open, tc.wantOpen)
			}
			if !strings.Contains(change, tc.wantChange) {
				t.Errorf("change tip = %q, want it to mention %q", change, tc.wantChange)
			}
		})
	}

	// A missing editor is named, so the user knows which one went away.
	open, _ := ideTooltips(ideChoice{set: true, missing: true,
		editor: ide.IDE{Name: "Cursor", Bundle: "/Applications/Cursor.app"}})
	if !strings.Contains(open, "Cursor") {
		t.Errorf("a missing editor should still be named: %q", open)
	}
}
