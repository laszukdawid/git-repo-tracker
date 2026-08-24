package loginitem

import (
	"strings"
	"testing"
)

func TestQuoteExecArg(t *testing.T) {
	cases := map[string]string{
		"/usr/local/bin/app":    "/usr/local/bin/app",
		"/Users/me/My Apps/app": `"/Users/me/My Apps/app"`,
		`/tmp/say "hi"`:         `"/tmp/say \"hi\""`,
		"/tmp/$HOME/app":        `"/tmp/\$HOME/app"`,
		"/tmp/100%done/app":     "/tmp/100%%done/app",
		`C:\weird\path`:         `"C:\\weird\\path"`,
		"/tmp/`uname`/app":      "\"/tmp/\\`uname\\`/app\"",
		"/tmp/a;rm -rf ~/app":   `"/tmp/a;rm -rf ~/app"`,
	}
	for in, want := range cases {
		if got := quoteExecArg(in); got != want {
			t.Errorf("quoteExecArg(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestRenderDesktopExecLine(t *testing.T) {
	out := renderDesktop([]string{"/opt/my tools/app", "--flag"})
	want := `Exec="/opt/my tools/app" --flag`
	if !strings.Contains(out, want+"\n") {
		t.Fatalf("desktop entry missing %q:\n%s", want, out)
	}
}

func TestRenderPlistEscapes(t *testing.T) {
	out := renderPlist([]string{"/a/<b>&c"})
	if !strings.Contains(out, "<string>/a/&lt;b&gt;&amp;c</string>") {
		t.Fatalf("plist not escaped:\n%s", out)
	}
}
