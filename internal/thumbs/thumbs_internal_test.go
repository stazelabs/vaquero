package thumbs

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
	"time"
)

// Every fast path reads the same colour as At.
func TestPixelsMatchAt(t *testing.T) {
	r := image.Rect(0, 0, 7, 5)
	fill := func(set func(x, y int, i uint8)) {
		for y := range 5 {
			for x := range 7 {
				set(x, y, uint8(x*31+y*17))
			}
		}
	}
	rgba, nrgba, nrgba64, gray, cmyk := image.NewRGBA(r), image.NewNRGBA(r), image.NewNRGBA64(r), image.NewGray(r), image.NewCMYK(r)
	pal := image.NewPaletted(r, color.Palette{color.Black, color.NRGBA{200, 10, 10, 128}, color.White})
	alpha := image.NewAlpha(r)
	ycc := image.NewYCbCr(r, image.YCbCrSubsampleRatio420)
	fill(func(x, y int, i uint8) {
		rgba.SetRGBA(x, y, color.RGBA{i / 2, i / 3, i / 4, i / 2})
		nrgba.SetNRGBA(x, y, color.NRGBA{i, 255 - i, i / 2, i})
		nrgba64.SetNRGBA64(x, y, color.NRGBA64{uint16(i) * 257, 1234, 65535 - uint16(i), uint16(i)*200 + 7})
		gray.SetGray(x, y, color.Gray{i})
		cmyk.SetCMYK(x, y, color.CMYK{i, 255 - i, i / 2, i / 5})
		pal.SetColorIndex(x, y, i%3)
		alpha.SetAlpha(x, y, color.Alpha{i})
		ycc.Y[ycc.YOffset(x, y)] = i
		ycc.Cb[ycc.COffset(x, y)] = 255 - i
		ycc.Cr[ycc.COffset(x, y)] = i / 2
	})
	for name, img := range map[string]image.Image{"rgba": rgba, "nrgba": nrgba, "nrgba64": nrgba64, "gray": gray,
		"cmyk": cmyk, "paletted": pal, "alpha": alpha, "ycbcr": ycc} {
		px := pixels(img)
		for y := range 5 {
			for x := range 7 {
				r1, g1, b1, a1 := px(x, y)
				r2, g2, b2, a2 := img.At(x, y).RGBA()
				if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
					t.Fatalf("%s at %d,%d: %d %d %d %d, At gives %d %d %d %d", name, x, y, r1, g1, b1, a1, r2, g2, b2, a2)
				}
			}
		}
	}
}

// Turning one way and back, or mirroring twice, gives the image again.
func TestOrientRoundTrips(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	for _, p := range [][2]int{{6, 8}, {8, 6}, {3, 3}, {2, 2}, {4, 4}, {5, 5}, {7, 7}} {
		if got := orient(orient(img, p[0]), p[1]); !bytes.Equal(got.Pix, img.Pix) || got.Bounds() != img.Bounds() {
			t.Errorf("%d then %d changed the image", p[0], p[1])
		}
	}
	if orient(img, 1) != img || orient(img, 9) != img {
		t.Error("1 and unknown values must leave it alone")
	}
}

type watchReader struct{ read bool }

func (w *watchReader) Read([]byte) (int, error) { w.read = true; return 0, errors.New("read") }

// Waiting for a slot ends with the context, before the source is read.
func TestMakeCancelledWhileQueued(t *testing.T) {
	for range maxMaking {
		making <- struct{}{}
	}
	defer func() {
		for range maxMaking {
			<-making
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	src := &watchReader{}
	start := time.Now()
	_, _, err := Make(ctx, src)
	if !errors.Is(err, context.DeadlineExceeded) || src.read {
		t.Fatalf("%v, read %v", err, src.read)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("waited %v", d)
	}
}
