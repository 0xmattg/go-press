package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateFaviconICO(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.png")
	dst := filepath.Join(dir, "public", "favicon.ico")

	img := image.NewRGBA(image.Rect(0, 0, 192, 192))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := GenerateFaviconICO(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 30 || !bytes.Equal(data[:6], []byte{0, 0, 1, 0, 1, 0}) {
		t.Fatalf("invalid ICO header")
	}
	if got := binary.LittleEndian.Uint32(data[18:22]); got != 22 {
		t.Fatalf("image offset = %d, want 22", got)
	}
	if !bytes.Equal(data[22:30], []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("ICO image payload is not PNG: %v", data[22:30])
	}
}

func TestGenerateResponsiveVariants(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "sample.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 120, A: 255})
		}
	}
	f, err := os.Create(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	variants, err := GenerateResponsiveVariants(srcPath, "/static/uploads/2026/04/sample.jpg", 7)
	if err != nil {
		t.Fatal(err)
	}
	var found480 bool
	for _, v := range variants {
		if v.Name == "480w" && v.Format == "jpeg" {
			found480 = true
			if v.Width != 480 || v.Height != 360 {
				t.Fatalf("480w dimensions = %dx%d, want 480x360", v.Width, v.Height)
			}
			if _, err := os.Stat(filepath.Join(dir, "sample-480w.jpg")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found480 {
		t.Fatalf("expected jpeg 480w variant, got %#v", variants)
	}
}
