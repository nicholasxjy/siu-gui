//go:build ignore

// Regenerates resources/icon.png from the SVG used by the in-app brand and
// favicon. Requires rsvg-convert (brew install librsvg / apt install librsvg2-bin).
// Run from the repository root: bun run generate:icon
//
// mygo converts this transparent 1024px PNG to macOS ICNS, multi-resolution
// Windows icons and Linux desktop icons when packaging. Do not redraw the
// geometry here: resources/icon.svg is the single design source.
package main

import (
	"bytes"
	"fmt"
	"image/png"
	"log"
	"os"
	"os/exec"
)

func main() {
	log.SetFlags(0)
	if err := generate(); err != nil {
		log.Fatal(err)
	}
}

func generate() error {
	const source = "resources/icon.svg"
	const output = "resources/icon.png"

	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("run from the repository root: %w", err)
	}
	renderer, err := exec.LookPath("rsvg-convert")
	if err != nil {
		return fmt.Errorf("install librsvg to regenerate icons (rsvg-convert): %w", err)
	}
	cmd := exec.Command(renderer, "--width", "1024", "--height", "1024", source)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("render icon: %w: %s", err, stderr.String())
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode rendered icon: %w", err)
	}
	if img.Bounds().Dx() != 1024 || img.Bounds().Dy() != 1024 {
		return fmt.Errorf("expected a 1024px square icon, got %v", img.Bounds())
	}
	for _, point := range [][2]int{{0, 0}, {1023, 0}, {0, 1023}, {1023, 1023}} {
		if _, _, _, alpha := img.At(point[0], point[1]).RGBA(); alpha != 0 {
			return fmt.Errorf("icon must have transparent corners")
		}
	}

	// Re-encode to strip renderer metadata and keep only portable PNG pixels.
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&out, img); err != nil {
		return fmt.Errorf("encode icon: %w", err)
	}
	if err := os.WriteFile(output, out.Bytes(), 0o644); err != nil {
		return err
	}
	log.Printf("wrote %s (1024×1024, transparent), from %s", output, source)
	return nil
}
