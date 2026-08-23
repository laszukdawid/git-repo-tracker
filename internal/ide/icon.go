package ide

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Reading a macOS application's own icon, so the row control shows IntelliJ's
// or VS Code's artwork rather than a generic glyph — which is the whole reason
// the chip is recognisable at a glance.
//
// This parses the .icns container directly rather than shelling out to sips or
// iconutil: those are a subprocess per icon on a path that runs while the
// popover is being built, and the container is simple enough that reading it is
// less code than managing the subprocess would be.

// pngMagic is the 8-byte signature every PNG starts with.
var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// IconPNG returns the editor's icon as PNG bytes, or an error when there is
// none to find. Only bundles have icons; a bare command has nothing to read.
func (i IDE) IconPNG() ([]byte, error) {
	if i.Bundle == "" {
		return nil, errors.New("no bundle to read an icon from")
	}
	path, err := iconFile(filepath.Join(i.Bundle, "Contents", "Resources"))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return largestPNGInICNS(data)
}

// iconFile picks the .icns to read from a bundle's Resources directory.
//
// The correct answer is in Info.plist under CFBundleIconFile, but that file is
// as often binary plist as XML, and parsing both to choose between candidates
// that are nearly always identical is not worth it. Most bundles ship exactly
// one .icns; when several exist the largest is the application icon and the
// others are document types.
func iconFile(resources string) (string, error) {
	entries, err := os.ReadDir(resources)
	if err != nil {
		return "", err
	}
	type candidate struct {
		path string
		size int64
	}
	var found []candidate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".icns") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		found = append(found, candidate{filepath.Join(resources, e.Name()), info.Size()})
	}
	if len(found) == 0 {
		return "", errors.New("no .icns in the bundle")
	}
	sort.Slice(found, func(a, b int) bool { return found[a].size > found[b].size })
	return found[0].path, nil
}

// largestPNGInICNS walks the container's chunks and returns the biggest PNG in
// it.
//
// An .icns is a header — the magic "icns" and a total length — followed by
// chunks of {4-byte type, 4-byte length including the header, payload}. Modern
// icons store their larger sizes as whole PNG files inside those payloads, so
// picking the largest PNG gives the sharpest artwork without having to know
// which type codes mean which pixel size. Older icons hold raw or run-length
// encoded bitmaps instead; those yield no PNG and the caller falls back to a
// generic glyph.
func largestPNGInICNS(data []byte) ([]byte, error) {
	if len(data) < 8 || string(data[0:4]) != "icns" {
		return nil, errors.New("not an icns file")
	}
	total := int(binary.BigEndian.Uint32(data[4:8]))
	if total > len(data) || total < 8 {
		total = len(data) // trust the file over a header that disagrees with it
	}

	var best []byte
	for off := 8; off+8 <= total; {
		size := int(binary.BigEndian.Uint32(data[off+4 : off+8]))
		if size < 8 || off+size > total {
			break // malformed; take whatever was found so far
		}
		payload := data[off+8 : off+size]
		if bytes.HasPrefix(payload, pngMagic) && len(payload) > len(best) {
			best = payload
		}
		off += size
	}
	if best == nil {
		return nil, errors.New("no PNG icon inside the icns")
	}
	return best, nil
}
