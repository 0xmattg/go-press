// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package media

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func testImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Left half red, right half blue, so the crop position shows.
			if x < w/2 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProcessAvatarCropsScalesAndReencodes(t *testing.T) {
	out, ext, err := ProcessAvatar(encodePNG(t, testImage(900, 600)))
	if err != nil || ext != ".png" {
		t.Fatalf("png = %q, %v", ext, err)
	}
	img, format, err := image.Decode(bytes.NewReader(out))
	if err != nil || format != "png" || img.Bounds().Dx() != AvatarSize || img.Bounds().Dy() != AvatarSize {
		t.Fatalf("decoded %s %v, %v", format, img.Bounds(), err)
	}
	// The center square of a red|blue image is still red on the left and
	// blue on the right.
	if r, _, b, _ := img.At(10, 128).RGBA(); r < 0xf000 || b > 0x1000 {
		t.Fatalf("left pixel = %v", img.At(10, 128))
	}
	if r, _, b, _ := img.At(245, 128).RGBA(); b < 0xf000 || r > 0x1000 {
		t.Fatalf("right pixel = %v", img.At(245, 128))
	}

	// A JPEG stays a JPEG; data appended to the upload does not survive.
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, testImage(120, 120), nil); err != nil {
		t.Fatal(err)
	}
	upload := append(jpg.Bytes(), []byte("<script>alert(1)</script>")...)
	out, ext, err = ProcessAvatar(upload)
	if err != nil || ext != ".jpg" || bytes.Contains(out, []byte("<script>")) {
		t.Fatalf("jpeg = %q, %v", ext, err)
	}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(out)); err != nil || format != "jpeg" || cfg.Width != AvatarSize {
		t.Fatalf("jpeg output %s %+v, %v", format, cfg, err)
	}
}

func TestProcessAvatarRefusesWhatItCannotUse(t *testing.T) {
	for name, tc := range map[string]struct {
		data []byte
		want error
	}{
		"not an image":   {[]byte("hello, I am text"), ErrAvatarFormat},
		"svg":            {[]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), ErrAvatarFormat},
		"too small":      {encodePNG(t, testImage(16, 16)), ErrAvatarSize},
		"too wide":       {encodePNG(t, image.NewGray(image.Rect(0, 0, AvatarMaxSide+1, 40))), ErrAvatarSize},
		"too many bytes": {make([]byte, MaxAvatarBytes+1), ErrAvatarTooLarge},
	} {
		if _, _, err := ProcessAvatar(tc.data); !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", name, err, tc.want)
		}
	}
}
