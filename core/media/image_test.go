package media

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

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
