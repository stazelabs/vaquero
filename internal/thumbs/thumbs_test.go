package thumbs_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"runtime"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/thumbs"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func opaque(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	return img
}

func decode(t *testing.T, data []byte) (image.Image, string) {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img, format
}

func TestMakeFitsAndKeepsAspect(t *testing.T) {
	for _, c := range []struct {
		name         string
		data         []byte
		wantW, wantH int
	}{
		{"wide png", encodePNG(t, opaque(1440, 900)), 192, 120},
		{"tall jpeg", encodeJPEG(t, opaque(300, 600)), 96, 192},
		{"small is never enlarged", encodePNG(t, opaque(100, 50)), 100, 50},
		{"square", encodePNG(t, opaque(400, 400)), 192, 192},
		{"thin line keeps a pixel", encodePNG(t, opaque(2000, 3)), 192, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			data, ctype, err := thumbs.Make(bytes.NewReader(c.data))
			if err != nil {
				t.Fatal(err)
			}
			img, _ := decode(t, data)
			if b := img.Bounds(); b.Dx() != c.wantW || b.Dy() != c.wantH {
				t.Fatalf("got %dx%d, want %dx%d", b.Dx(), b.Dy(), c.wantW, c.wantH)
			}
			if ctype != "image/jpeg" {
				t.Fatalf("opaque source gave %q", ctype)
			}
		})
	}
}

// The average of a box is its pixels' mean: a 2x2 checkerboard of black
// and white scaled to 1x1 is mid grey.
func TestMakeAveragesTheArea(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 384, 384))
	for y := range 384 {
		for x := range 384 {
			if (x+y)%2 == 0 {
				src.SetGray(x, y, color.Gray{255})
			}
		}
	}
	data, _, err := thumbs.Make(bytes.NewReader(encodePNG(t, src)))
	if err != nil {
		t.Fatal(err)
	}
	img, _ := decode(t, data)
	r, g, b, _ := img.At(96, 96).RGBA()
	for _, v := range []uint32{r >> 8, g >> 8, b >> 8} {
		if v < 120 || v > 135 {
			t.Fatalf("want mid grey, got %d %d %d", r>>8, g>>8, b>>8)
		}
	}
}

func TestMakePNGKeepsAlpha(t *testing.T) {
	src := opaque(400, 200)
	for y := range 200 {
		for x := range 200 {
			src.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 0})
		}
	}
	data, ctype, err := thumbs.Make(bytes.NewReader(encodePNG(t, src)))
	if err != nil || ctype != "image/png" {
		t.Fatalf("%q %v", ctype, err)
	}
	img, format := decode(t, data)
	if format != "png" || img.Bounds().Dx() != 192 || img.Bounds().Dy() != 96 {
		t.Fatalf("%s %v", format, img.Bounds())
	}
	if _, _, _, a := img.At(10, 10).RGBA(); a != 0 {
		t.Fatalf("transparent corner has alpha %d", a)
	}
	if _, _, _, a := img.At(180, 10).RGBA(); a != 0xffff {
		t.Fatalf("opaque side has alpha %d", a)
	}
}

func TestMakeJPEGForOpaque(t *testing.T) {
	// A GIF's first frame, opaque, comes out as JPEG.
	pal := color.Palette{color.Black, color.White}
	frame := image.NewPaletted(image.Rect(0, 0, 300, 300), pal)
	var g bytes.Buffer
	if err := gif.EncodeAll(&g, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string][]byte{"png": encodePNG(t, opaque(500, 500)), "gif": g.Bytes()} {
		data, ctype, err := thumbs.Make(bytes.NewReader(src))
		if err != nil || ctype != "image/jpeg" {
			t.Fatalf("%s: %q %v", name, ctype, err)
		}
		if _, format := decode(t, data); format != "jpeg" {
			t.Fatalf("%s: encoded as %s", name, format)
		}
	}
}

// pngHeader is a PNG signature and an IHDR chunk claiming w x h, with no
// image data: DecodeConfig reads it, Decode would need the pixels.
func pngHeader(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

// Review Focus 3: a hostile or huge image is refused without decoding it.
func TestMakeRefusesHugeAndBogus(t *testing.T) {
	huge := pngHeader(10_000, 5_000) // 50 megapixels
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, _, err := thumbs.Make(bytes.NewReader(huge))
	runtime.ReadMemStats(&after)
	if !errors.Is(err, thumbs.ErrTooLarge) {
		t.Fatalf("50 megapixels: %v", err)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Fatalf("refusing allocated %d bytes", grew)
	}

	jpg := encodeJPEG(t, opaque(300, 300))
	webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00" + strings.Repeat("\x00", 24))
	for name, c := range map[string]struct {
		data        []byte
		unsupported bool
	}{
		"text named .png": {[]byte("this is not an image, only words"), true},
		"webp":            {webp, true},
		"empty":           {nil, true},
		"truncated jpeg":  {jpg[:len(jpg)/2], false},
		"png header only": {pngHeader(100, 100), false},
		"zero width":      {pngHeader(0, 100), false},
	} {
		data, ctype, err := thumbs.Make(bytes.NewReader(c.data))
		if err == nil || data != nil || ctype != "" {
			t.Errorf("%s: want an error, got %d bytes %q", name, len(data), ctype)
			continue
		}
		if errors.Is(err, thumbs.ErrUnsupported) != c.unsupported {
			t.Errorf("%s: %v", name, err)
		}
	}
}
