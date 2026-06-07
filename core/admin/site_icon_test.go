package admin

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"go-press/config"
)

func TestSyncSiteIconGeneratesAndRemovesPublicFavicon(t *testing.T) {
	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	publicDir := filepath.Join(root, "public")
	sourcePath := filepath.Join(uploadDir, "2026", "06", "icon.png")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	img.Set(0, 0, color.RGBA{R: 20, G: 40, B: 60, A: 255})
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		config:        config.CMSConfig{UploadDir: uploadDir},
		sitePublicDir: publicDir,
	}
	if err := svc.SyncSiteIcon("/uploads/2026/06/icon.png"); err != nil {
		t.Fatal(err)
	}

	faviconPath := filepath.Join(publicDir, "favicon.ico")
	if info, err := os.Stat(faviconPath); err != nil || info.Size() == 0 {
		t.Fatalf("favicon was not generated: info=%v err=%v", info, err)
	}

	if err := svc.SyncSiteIcon(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(faviconPath); !os.IsNotExist(err) {
		t.Fatalf("favicon should be removed when setting is cleared: %v", err)
	}
}
