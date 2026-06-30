// Command gen rasterizes internal/assets/icon.svg into icon.png at the repo root,
// for use as the macOS .app bundle icon (`task bundle`). Run via `task icon`.
package main

import (
	"image"
	"image/png"
	"log"
	"os"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

const size = 512

func main() {
	in, err := os.Open("internal/assets/icon.svg")
	if err != nil {
		log.Fatalf("open svg: %v", err)
	}
	defer in.Close()

	icon, err := oksvg.ReadIconStream(in)
	if err != nil {
		log.Fatalf("parse svg: %v", err)
	}
	icon.SetTarget(0, 0, float64(size), float64(size))

	rgba := image.NewRGBA(image.Rect(0, 0, size, size))
	scanner := rasterx.NewScannerGV(size, size, rgba, rgba.Bounds())
	raster := rasterx.NewDasher(size, size, scanner)
	icon.Draw(raster, 1.0)

	out, err := os.Create("icon.png")
	if err != nil {
		log.Fatalf("create png: %v", err)
	}
	defer out.Close()
	if err := png.Encode(out, rgba); err != nil {
		log.Fatalf("encode png: %v", err)
	}
	log.Printf("wrote icon.png (%dx%d)", size, size)
}
