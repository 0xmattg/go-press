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
)

// Avatar limits: uploads larger than MaxAvatarBytes or AvatarMaxSide pixels
// on a side are refused; every avatar is stored as an AvatarSize square.
const (
	MaxAvatarBytes = 5 << 20
	AvatarMaxSide  = 6000
	AvatarMinSide  = 32
	AvatarSize     = 256
)

// AvatarAccept lists the media types ProcessAvatar takes, in the form of
// an <input type="file"> accept attribute.
const AvatarAccept = "image/jpeg,image/png,image/gif"

// Avatar errors.
var (
	ErrAvatarTooLarge = errors.New("media: the avatar is too large")
	ErrAvatarFormat   = errors.New("media: the avatar must be a JPEG, PNG or GIF image")
	ErrAvatarSize     = errors.New("media: the avatar's dimensions are out of range")
)

// ProcessAvatar turns an uploaded image into a stored avatar: it accepts
// JPEG, PNG and GIF (first frame), checks the size before decoding the
// pixels, crops the center square and scales it to AvatarSize, and encodes
// it again, so no metadata or extra data of the upload survives. It returns
// the encoded image and its file extension (".jpg" for JPEG uploads, ".png"
// otherwise, keeping transparency).
func ProcessAvatar(data []byte) ([]byte, string, error) {
	if len(data) > MaxAvatarBytes {
		return nil, "", ErrAvatarTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrAvatarFormat
	}
	switch format {
	case "jpeg", "png", "gif":
	default:
		return nil, "", ErrAvatarFormat
	}
	if cfg.Width < AvatarMinSide || cfg.Height < AvatarMinSide || cfg.Width > AvatarMaxSide || cfg.Height > AvatarMaxSide {
		return nil, "", ErrAvatarSize
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrAvatarFormat
	}
	square := cropSquare(img, AvatarSize)
	var out bytes.Buffer
	if format == "jpeg" {
		err = jpeg.Encode(&out, square, &jpeg.Options{Quality: 88})
		return out.Bytes(), ".jpg", err
	}
	err = png.Encode(&out, square)
	return out.Bytes(), ".png", err
}

// cropSquare scales the center square of src to size×size. It averages every
// source pixel under a destination pixel when shrinking, so large photos do
// not alias, and interpolates when the source is smaller.
func cropSquare(src image.Image, size int) *image.RGBA {
	b := src.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	if side <= size {
		return resizeBilinear(src, size, size, true)
	}
	x0 := b.Min.X + (b.Dx()-side)/2
	y0 := b.Min.Y + (b.Dy()-side)/2
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	for dy := 0; dy < size; dy++ {
		sy0, sy1 := y0+dy*side/size, y0+(dy+1)*side/size
		for dx := 0; dx < size; dx++ {
			sx0, sx1 := x0+dx*side/size, x0+(dx+1)*side/size
			var r, g, bl, a, n uint64
			for y := sy0; y < sy1; y++ {
				for x := sx0; x < sx1; x++ {
					cr, cg, cb, ca := src.At(x, y).RGBA()
					r, g, bl, a = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca)
					n++
				}
			}
			dst.Set(dx, dy, color.RGBA64{R: uint16(r / n), G: uint16(g / n), B: uint16(bl / n), A: uint16(a / n)})
		}
	}
	return dst
}
