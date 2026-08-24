package ide

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIDEIDRoundTrip(t *testing.T) {
	bundle := IDE{Name: "Cursor", Bundle: "/Applications/Cursor.app"}
	if bundle.ID() != "/Applications/Cursor.app" {
		t.Errorf("bundle id = %q", bundle.ID())
	}
	back, ok := Lookup(bundle.ID(), nil)
	if !ok || back.Bundle != bundle.Bundle || back.Name != "Cursor" {
		t.Errorf("bundle round-trip = %+v (%v)", back, ok)
	}

	cmd := IDE{Name: "Visual Studio Code", Command: "code"}
	if cmd.ID() != "cmd:code" {
		t.Errorf("command id = %q", cmd.ID())
	}
	back, ok = Lookup(cmd.ID(), nil)
	if !ok || back.Command != "code" {
		t.Errorf("command round-trip = %+v (%v)", back, ok)
	}
	// The display name is recovered from the known list, not left as the binary.
	if back.Name != "Visual Studio Code" {
		t.Errorf("command name = %q, want the editor's real name", back.Name)
	}

	if _, ok := Lookup("", nil); ok {
		t.Error("an empty id should not resolve")
	}
}

// A remembered editor that has been deleted must still resolve, so the UI can
// name it while reporting that it is gone. Resolving to nothing would leave the
// user with a control that silently stopped working.
func TestLookupKeepsNameOfAMissingEditor(t *testing.T) {
	got, ok := Lookup("/Applications/Deleted Editor.app", nil)
	if !ok {
		t.Fatal("a missing editor should still resolve")
	}
	if got.Name != "Deleted Editor" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Exists() {
		t.Error("Exists must report the truth")
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Fake.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if !(IDE{Bundle: app}).Exists() {
		t.Error("an existing bundle should exist")
	}
	if (IDE{Bundle: filepath.Join(dir, "Nope.app")}).Exists() {
		t.Error("a missing bundle should not exist")
	}
	if (IDE{}).Exists() {
		t.Error("an empty editor should not exist")
	}
	// A file is not a bundle.
	plain := filepath.Join(dir, "notabundle.app")
	if err := os.WriteFile(plain, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if (IDE{Bundle: plain}).Exists() {
		t.Error("a plain file is not a bundle")
	}
}

func TestArgs(t *testing.T) {
	// A path with spaces must stay one argument — nothing goes through a shell.
	got, err := (IDE{Bundle: "/Applications/Cursor.app"}).Args("/w/my repo")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"open", "-a", "/Applications/Cursor.app", "/w/my repo"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("bundle args = %v", got)
	}

	got, err = (IDE{Command: "code"}).Args("/w/my repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "code" || got[1] != "/w/my repo" {
		t.Errorf("command args = %v", got)
	}

	if _, err := (IDE{}).Args("/w"); err == nil {
		t.Error("an editor with neither a bundle nor a command has no command line")
	}
}

func TestIsKnownEditor(t *testing.T) {
	yes := []string{"IntelliJ IDEA", "IntelliJ IDEA Ultimate", "PyCharm Community Edition",
		"Visual Studio Code", "Cursor", "Zed", "Xcode"}
	no := []string{"Safari", "Mail", "Slack", "IntelliJIDEA", "Codebase", "Notes"}
	for _, name := range yes {
		if !isKnownEditor(name) {
			t.Errorf("%q should be recognised as an editor", name)
		}
	}
	for _, name := range no {
		if isKnownEditor(name) {
			t.Errorf("%q should not be recognised as an editor", name)
		}
	}
}

func TestFromPath(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "My Editor.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := FromPath(app)
	if !ok || got.Name != "My Editor" || got.Bundle != app {
		t.Errorf("bundle = %+v (%v)", got, ok)
	}

	bin := filepath.Join(dir, "myeditor")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok = FromPath(bin)
	if !ok || got.Command != bin {
		t.Errorf("executable = %+v (%v)", got, ok)
	}

	if _, ok := FromPath(""); ok {
		t.Error("an empty path is not an editor")
	}
	if _, ok := FromPath(filepath.Join(dir, "missing")); ok {
		t.Error("a path that does not exist is not an editor")
	}
	if _, ok := FromPath(dir); ok {
		t.Error("a plain directory is not an editor")
	}
}

// Discovery must include an editor the user pointed at by hand even though its
// name is nothing the known list would match.
func TestDiscoverIncludesManualEntries(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Obscure Editor.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, i := range Discover([]string{app}) {
		if i.Bundle == app {
			return
		}
	}
	t.Error("a manually added editor was not offered")
}

func TestLargestPNGInICNS(t *testing.T) {
	small := fakePNG(40)
	large := fakePNG(200)
	data := buildICNS(map[string][]byte{
		"ic07": small,
		"ic09": large,
		"is32": []byte("raw bitmap, not a png"),
	})

	got, err := largestPNGInICNS(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, large) {
		t.Errorf("picked a %d-byte chunk, want the %d-byte one", len(got), len(large))
	}

	if _, err := largestPNGInICNS([]byte("not an icns at all")); err == nil {
		t.Error("a non-icns file should be rejected")
	}
	// An icon holding only old-style bitmaps yields nothing, and the caller falls
	// back to a generic glyph rather than drawing garbage.
	onlyRaw := buildICNS(map[string][]byte{"is32": []byte("raw")})
	if _, err := largestPNGInICNS(onlyRaw); err == nil {
		t.Error("an icns with no PNG should report that")
	}
}

func TestIconPNGNeedsABundle(t *testing.T) {
	if _, err := (IDE{Command: "code"}).IconPNG(); err == nil {
		t.Error("a bare command has no icon to read")
	}
}

func TestDiscoverDoesNotPanicWithoutApplications(t *testing.T) {
	if runtime.GOOS != "darwin" {
		// Off macOS there are no bundles to scan; discovery is PATH-only.
		if dirs := appDirs(); dirs != nil {
			t.Errorf("appDirs = %v, want none off macOS", dirs)
		}
	}
	_ = Discover(nil) // must not panic on any platform
}

// fakePNG builds something that starts with the PNG signature and is n bytes long.
func fakePNG(n int) []byte {
	b := make([]byte, n)
	copy(b, pngMagic)
	for i := len(pngMagic); i < n; i++ {
		b[i] = byte(i)
	}
	return b
}

// buildICNS assembles a minimal icns container around the given chunks.
func buildICNS(chunks map[string][]byte) []byte {
	var body []byte
	// Sorted iteration is unnecessary here — the parser scans every chunk.
	for typ, payload := range chunks {
		head := make([]byte, 8)
		copy(head, typ)
		binary.BigEndian.PutUint32(head[4:], uint32(8+len(payload)))
		body = append(body, head...)
		body = append(body, payload...)
	}
	out := make([]byte, 8, 8+len(body))
	copy(out, "icns")
	binary.BigEndian.PutUint32(out[4:], uint32(8+len(body)))
	return append(out, body...)
}
