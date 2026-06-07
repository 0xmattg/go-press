package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxImagePixels = 80_000_000

// ThumbnailSize defines a named thumbnail size.
type ThumbnailSize struct {
	Name   string
	Width  int
	Height int
	Crop   bool
}

// DefaultSizes returns the standard thumbnail sizes.
func DefaultSizes() []ThumbnailSize {
	return []ThumbnailSize{
		{Name: "thumbnail", Width: 150, Height: 150, Crop: true},
		{Name: "medium", Width: 300, Height: 300, Crop: false},
		{Name: "large", Width: 1024, Height: 1024, Crop: false},
	}
}

// ResponsiveSizes returns the front-end image variants generated for uploads.
func ResponsiveSizes() []ThumbnailSize {
	return []ThumbnailSize{
		{Name: "thumb", Width: 150, Height: 150, Crop: true},
		{Name: "480w", Width: 480, Height: 0, Crop: false},
		{Name: "768w", Width: 768, Height: 0, Crop: false},
		{Name: "1024w", Width: 1024, Height: 0, Crop: false},
		{Name: "1440w", Width: 1440, Height: 0, Crop: false},
	}
}

// GetImageDimensions reads an image file and returns its width and height.
func GetImageDimensions(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxImagePixels {
		return 0, 0, fmt.Errorf("image dimensions are invalid or too large: %dx%d", cfg.Width, cfg.Height)
	}
	return cfg.Width, cfg.Height, nil
}

// GenerateFaviconICO creates a single-image ICO file containing PNG data.
// Images larger than 256 pixels are scaled down while preserving aspect ratio.
func GenerateFaviconICO(srcPath, dstPath string) error {
	img, _, err := decodeImage(srcPath)
	if err != nil {
		return err
	}

	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	if width <= 0 || height <= 0 {
		return fmt.Errorf("favicon source has invalid dimensions")
	}
	if width > 256 || height > 256 {
		scale := math.Min(256/float64(width), 256/float64(height))
		width = int(math.Round(float64(width) * scale))
		height = int(math.Round(float64(height) * scale))
		if width < 1 {
			width = 1
		}
		if height < 1 {
			height = 1
		}
		img = resizeBilinear(img, width, height, false)
	}

	var pngData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dstPath), ".favicon-*.ico")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := writeICO(tmp, width, height, pngData.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dstPath)
}

func writeICO(dst *os.File, width, height int, imageData []byte) error {
	if _, err := dst.Write([]byte{0, 0, 1, 0, 1, 0}); err != nil {
		return err
	}

	entry := make([]byte, 16)
	if width < 256 {
		entry[0] = byte(width)
	}
	if height < 256 {
		entry[1] = byte(height)
	}
	binary.LittleEndian.PutUint16(entry[4:6], 1)
	binary.LittleEndian.PutUint16(entry[6:8], 32)
	binary.LittleEndian.PutUint32(entry[8:12], uint32(len(imageData)))
	binary.LittleEndian.PutUint32(entry[12:16], 22)
	if _, err := dst.Write(entry); err != nil {
		return err
	}
	_, err := dst.Write(imageData)
	return err
}

// GenerateResponsiveVariants creates resized derivatives next to the original file.
func GenerateResponsiveVariants(srcPath, publicPath string, mediaID uint) ([]MediaVariant, error) {
	img, format, err := decodeImage(srcPath)
	if err != nil {
		return nil, err
	}
	srcBounds := img.Bounds()
	srcW, srcH := srcBounds.Dx(), srcBounds.Dy()
	if srcW <= 0 || srcH <= 0 || srcW*srcH > maxImagePixels {
		return nil, fmt.Errorf("image dimensions are invalid or too large: %dx%d", srcW, srcH)
	}

	var variants []MediaVariant
	for _, size := range ResponsiveSizes() {
		w, h := variantDimensions(srcW, srcH, size)
		if w <= 0 || h <= 0 {
			continue
		}
		resized := resizeBilinear(img, w, h, size.Crop)
		originalVariant, err := writeOriginalFormatVariant(srcPath, publicPath, mediaID, resized, size.Name, format)
		if err != nil {
			return variants, err
		}
		variants = append(variants, originalVariant)

		if webpVariant, err := writeWebPVariant(srcPath, publicPath, mediaID, size.Name, w, h); err == nil {
			variants = append(variants, webpVariant)
		}
	}
	// Keep one WebP candidate at source width so high-DPR layouts can choose a ceiling candidate.
	if webpVariant, err := writeWebPVariant(srcPath, publicPath, mediaID, "full", srcW, srcH); err == nil {
		variants = append(variants, webpVariant)
	}
	return variants, nil
}

// GenerateThumbnail creates a resized version of an image and returns its path.
func GenerateThumbnail(srcPath string, size ThumbnailSize) (string, error) {
	img, format, err := decodeImage(srcPath)
	if err != nil {
		return "", err
	}
	w, h := variantDimensions(img.Bounds().Dx(), img.Bounds().Dy(), size)
	if w <= 0 || h <= 0 {
		return srcPath, nil
	}
	resized := resizeBilinear(img, w, h, size.Crop)
	ext := filepath.Ext(srcPath)
	base := strings.TrimSuffix(srcPath, ext)
	outPath := fmt.Sprintf("%s-%s%s", base, size.Name, ext)
	if err := encodeImage(outPath, resized, format); err != nil {
		return "", err
	}
	return outPath, nil
}

// SanitizeFilename cleans a filename for safe storage.
func SanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, " ", "-")
	var safe strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			safe.WriteRune(r)
		}
	}
	return safe.String()
}

func decodeImage(path string) (image.Image, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, "", err
	}
	return img, format, nil
}

func variantDimensions(srcW, srcH int, size ThumbnailSize) (int, int) {
	if size.Crop {
		if srcW <= size.Width && srcH <= size.Height {
			return 0, 0
		}
		return size.Width, size.Height
	}
	if size.Width <= 0 {
		return 0, 0
	}
	if srcW <= size.Width {
		return 0, 0
	}
	scale := float64(size.Width) / float64(srcW)
	return size.Width, int(math.Round(float64(srcH) * scale))
}

func writeOriginalFormatVariant(srcPath, publicPath string, mediaID uint, img image.Image, name, format string) (MediaVariant, error) {
	ext := filepath.Ext(srcPath)
	base := strings.TrimSuffix(srcPath, ext)
	outPath := fmt.Sprintf("%s-%s%s", base, name, ext)
	if err := encodeImage(outPath, img, format); err != nil {
		return MediaVariant{}, err
	}
	info, err := os.Stat(outPath)
	if err != nil {
		return MediaVariant{}, err
	}
	outPublicPath := fmt.Sprintf("%s-%s%s", strings.TrimSuffix(publicPath, filepath.Ext(publicPath)), name, ext)
	return MediaVariant{
		MediaID:  mediaID,
		Name:     name,
		Format:   normalizeImageFormat(format),
		MimeType: mimeTypeForFormat(format),
		Path:     outPublicPath,
		Width:    img.Bounds().Dx(),
		Height:   img.Bounds().Dy(),
		Size:     info.Size(),
	}, nil
}

func writeWebPVariant(srcPath, publicPath string, mediaID uint, name string, width, height int) (MediaVariant, error) {
	cwebp, err := exec.LookPath("cwebp")
	if err != nil {
		return MediaVariant{}, err
	}
	ext := filepath.Ext(srcPath)
	base := strings.TrimSuffix(srcPath, ext)
	outPath := fmt.Sprintf("%s-%s.webp", base, name)
	cmd := exec.Command(cwebp, "-quiet", "-q", "78", "-resize", fmt.Sprint(width), fmt.Sprint(height), srcPath, "-o", outPath)
	if err := cmd.Run(); err != nil {
		return MediaVariant{}, err
	}
	info, err := os.Stat(outPath)
	if err != nil {
		return MediaVariant{}, err
	}
	outPublicPath := fmt.Sprintf("%s-%s.webp", strings.TrimSuffix(publicPath, filepath.Ext(publicPath)), name)
	return MediaVariant{
		MediaID:  mediaID,
		Name:     name,
		Format:   "webp",
		MimeType: "image/webp",
		Path:     outPublicPath,
		Width:    width,
		Height:   height,
		Size:     info.Size(),
	}, nil
}

// WebPEncoderAvailable reports whether cwebp is available in PATH.
func WebPEncoderAvailable() bool {
	_, err := exec.LookPath("cwebp")
	return err == nil
}

func encodeImage(path string, img image.Image, format string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	switch normalizeImageFormat(format) {
	case "jpeg":
		return jpeg.Encode(f, img, &jpeg.Options{Quality: 84})
	case "png":
		return png.Encode(f, img)
	default:
		return jpeg.Encode(f, img, &jpeg.Options{Quality: 84})
	}
}

func resizeBilinear(src image.Image, dstW, dstH int, crop bool) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	srcBounds := src.Bounds()
	srcW, srcH := srcBounds.Dx(), srcBounds.Dy()

	scaleX := float64(srcW) / float64(dstW)
	scaleY := float64(srcH) / float64(dstH)
	offsetX, offsetY := 0.0, 0.0
	if crop {
		scale := math.Min(scaleX, scaleY)
		offsetX = (float64(srcW) - float64(dstW)*scale) / 2
		offsetY = (float64(srcH) - float64(dstH)*scale) / 2
		scaleX, scaleY = scale, scale
	}

	for y := 0; y < dstH; y++ {
		fy := offsetY + (float64(y)+0.5)*scaleY - 0.5
		y0 := clampInt(int(math.Floor(fy)), 0, srcH-1)
		y1 := clampInt(y0+1, 0, srcH-1)
		wy := fy - math.Floor(fy)
		for x := 0; x < dstW; x++ {
			fx := offsetX + (float64(x)+0.5)*scaleX - 0.5
			x0 := clampInt(int(math.Floor(fx)), 0, srcW-1)
			x1 := clampInt(x0+1, 0, srcW-1)
			wx := fx - math.Floor(fx)
			c00 := src.At(srcBounds.Min.X+x0, srcBounds.Min.Y+y0)
			c10 := src.At(srcBounds.Min.X+x1, srcBounds.Min.Y+y0)
			c01 := src.At(srcBounds.Min.X+x0, srcBounds.Min.Y+y1)
			c11 := src.At(srcBounds.Min.X+x1, srcBounds.Min.Y+y1)
			dst.Set(x, y, bilinearColor(c00, c10, c01, c11, wx, wy))
		}
	}
	return dst
}

func bilinearColor(c00, c10, c01, c11 color.Color, wx, wy float64) color.Color {
	r00, g00, b00, a00 := c00.RGBA()
	r10, g10, b10, a10 := c10.RGBA()
	r01, g01, b01, a01 := c01.RGBA()
	r11, g11, b11, a11 := c11.RGBA()
	return rgba64{
		r: blend4(r00, r10, r01, r11, wx, wy),
		g: blend4(g00, g10, g01, g11, wx, wy),
		b: blend4(b00, b10, b01, b11, wx, wy),
		a: blend4(a00, a10, a01, a11, wx, wy),
	}
}

type rgba64 struct{ r, g, b, a uint32 }

func (c rgba64) RGBA() (uint32, uint32, uint32, uint32) { return c.r, c.g, c.b, c.a }

func blend4(v00, v10, v01, v11 uint32, wx, wy float64) uint32 {
	top := float64(v00)*(1-wx) + float64(v10)*wx
	bottom := float64(v01)*(1-wx) + float64(v11)*wx
	return uint32(top*(1-wy) + bottom*wy)
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func normalizeImageFormat(format string) string {
	format = strings.ToLower(format)
	if format == "jpg" {
		return "jpeg"
	}
	return format
}

func mimeTypeForFormat(format string) string {
	switch normalizeImageFormat(format) {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
