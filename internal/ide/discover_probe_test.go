package ide

import (
	"os"
	"testing"
)

// Prints what this machine actually has, for a human to sanity-check the
// detection list. Skipped unless GRT_IDE_PROBE is set.
func TestProbeThisMachine(t *testing.T) {
	if os.Getenv("GRT_IDE_PROBE") == "" {
		t.Skip("set GRT_IDE_PROBE to list the editors on this machine")
	}
	for _, i := range Discover(nil) {
		icon := "no icon"
		if b, err := i.IconPNG(); err == nil {
			_ = b
			icon = "icon ok"
		} else {
			icon = "no icon: " + err.Error()
		}
		t.Logf("%-30s exists=%v  %-45s %s", i.Name, i.Exists(), i.ID(), icon)
	}
}
