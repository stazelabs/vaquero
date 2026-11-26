// Package thumbs makes the small previews the ticket page shows for image
// attachments. It uses only the standard library's decoders: PNG, JPEG and
// GIF (a GIF's first frame). WebP and every other type are ErrUnsupported,
// and the page shows the original instead.
//
// Memory is bounded before anything is decoded: image.DecodeConfig reads
// only the header, and an image over MaxPixels is ErrTooLarge. At most
// maxMaking images are decoded at once, so a burst of first requests for a
// gallery can't hold many full-size images in memory together.
//
// A thumbnail fits within Max × Max, keeps the aspect ratio and is never
// larger than the source. It is a PNG when any of its pixels is not fully
// opaque, else a JPEG at quality 85.
package thumbs

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"

	// Registered for image.DecodeConfig and image.Decode.
	_ "image/gif"
)

const (
	// Max is the longest side of a thumbnail, in pixels.
	Max = 192
	// MaxPixels is the largest source image, in pixels, that is decoded.
	MaxPixels = 40_000_000
	// maxSource bounds the bytes read; attachments are at most 10 MB.
	maxSource = 16 << 20
	// maxMaking is how many images may be decoded at once.
	maxMaking = 2
)

var (
	// ErrUnsupported is a source that is not a PNG, JPEG or GIF.
	ErrUnsupported = errors.New("thumbs: unsupported image type")
	// ErrTooLarge is a source over MaxPixels.
	ErrTooLarge = errors.New("thumbs: image too large")
)

var making = make(chan struct{}, maxMaking)

// Make reads an image from r and returns its thumbnail and content type
// ("image/png" or "image/jpeg"). It returns ErrUnsupported for a type it
// can't decode, ErrTooLarge over MaxPixels, and a decode error for a
// damaged image.
func Make(r io.Reader) ([]byte, string, error) {
	src, err := io.ReadAll(io.LimitReader(r, maxSource))
	if err != nil {
		return nil, "", fmt.Errorf("thumbs: read: %w", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(src))
	switch {
	case errors.Is(err, image.ErrFormat):
		return nil, "", ErrUnsupported
	case err != nil:
		return nil, "", fmt.Errorf("thumbs: %w", err)
	case cfg.Width <= 0 || cfg.Height <= 0:
		return nil, "", errors.New("thumbs: empty image")
	case int64(cfg.Width)*int64(cfg.Height) > MaxPixels:
		return nil, "", ErrTooLarge
	}
	making <- struct{}{}
	defer func() { <-making }()
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, "", fmt.Errorf("thumbs: %w", err)
	}
	out, opaque := scale(img)
	var b bytes.Buffer
	if opaque {
		if err := jpeg.Encode(&b, out, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", fmt.Errorf("thumbs: encode: %w", err)
		}
		return b.Bytes(), "image/jpeg", nil
	}
	if err := png.Encode(&b, out); err != nil {
		return nil, "", fmt.Errorf("thumbs: encode: %w", err)
	}
	return b.Bytes(), "image/png", nil
}

// fit is the size of a w × h image scaled to fit within Max × Max, never
// enlarged, each side at least one pixel.
func fit(w, h int) (int, int) {
	if w <= Max && h <= Max {
		return w, h
	}
	if w >= h {
		return Max, max(1, int(int64(h)*Max/int64(w)))
	}
	return max(1, int(int64(w)*Max/int64(h))), Max
}

// scale downsizes img to fit and reports whether every pixel of the
// result is fully opaque.
func scale(img image.Image) (*image.RGBA, bool) {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw, dh := fit(sw, sh)
	px := pixels(img)
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	opaque := true
	// Area averaging: each output pixel covers the box of source pixels
	// from x*sw/dw up to (x+1)*sw/dw across and the same down. Since the
	// result is never larger than the source, every box holds at least
	// one pixel, and the boxes tile the source exactly, so every source
	// pixel counts once. The output pixel is the plain mean of its box's
	// pixels, taken in premultiplied colour so a transparent pixel adds
	// no colour of its own.
	for y := range dh {
		y0, y1 := y*sh/dh, (y+1)*sh/dh
		for x := range dw {
			x0, x1 := x*sw/dw, (x+1)*sw/dw
			var r, g, bl, a uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					pr, pg, pb, pa := px(b.Min.X+sx, b.Min.Y+sy)
					r, g, bl, a = r+uint64(pr), g+uint64(pg), bl+uint64(pb), a+uint64(pa)
				}
			}
			n := uint64((x1 - x0) * (y1 - y0))
			c := color.RGBA64{R: uint16(r / n), G: uint16(g / n), B: uint16(bl / n), A: uint16(a / n)}
			if c.A != 0xffff {
				opaque = false
			}
			out.SetRGBA64(x, y, c)
		}
	}
	return out, opaque
}

// pixels returns a reader of img's premultiplied 16-bit colour at (x, y),
// reading the decoders' own pixel layouts directly where it can (At boxes
// every colour it returns, which is slow over millions of pixels).
func pixels(img image.Image) func(x, y int) (r, g, b, a uint32) {
	switch m := img.(type) {
	case *image.YCbCr:
		return func(x, y int) (uint32, uint32, uint32, uint32) {
			yi, ci := m.YOffset(x, y), m.COffset(x, y)
			return color.YCbCr{Y: m.Y[yi], Cb: m.Cb[ci], Cr: m.Cr[ci]}.RGBA()
		}
	case *image.RGBA:
		return func(x, y int) (uint32, uint32, uint32, uint32) {
			i := m.PixOffset(x, y)
			s := m.Pix[i : i+4 : i+4]
			return uint32(s[0]) * 0x101, uint32(s[1]) * 0x101, uint32(s[2]) * 0x101, uint32(s[3]) * 0x101
		}
	case *image.NRGBA:
		return func(x, y int) (uint32, uint32, uint32, uint32) {
			i := m.PixOffset(x, y)
			s := m.Pix[i : i+4 : i+4]
			return color.NRGBA{R: s[0], G: s[1], B: s[2], A: s[3]}.RGBA()
		}
	case *image.Gray:
		return func(x, y int) (uint32, uint32, uint32, uint32) {
			v := uint32(m.Pix[m.PixOffset(x, y)]) * 0x101
			return v, v, v, 0xffff
		}
	case *image.Paletted:
		rgba := make([][4]uint32, len(m.Palette))
		for i, c := range m.Palette {
			r, g, b, a := c.RGBA()
			rgba[i] = [4]uint32{r, g, b, a}
		}
		return func(x, y int) (uint32, uint32, uint32, uint32) {
			i := int(m.Pix[m.PixOffset(x, y)])
			if i >= len(rgba) {
				return 0, 0, 0, 0xffff
			}
			c := rgba[i]
			return c[0], c[1], c[2], c[3]
		}
	default:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return img.At(x, y).RGBA() }
	}
}
