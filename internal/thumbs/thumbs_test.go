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
	"time"

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
			data, ctype, err := thumbs.Make(t.Context(), bytes.NewReader(c.data))
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
	data, _, err := thumbs.Make(t.Context(), bytes.NewReader(encodePNG(t, src)))
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
	data, ctype, err := thumbs.Make(t.Context(), bytes.NewReader(encodePNG(t, src)))
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
		data, ctype, err := thumbs.Make(t.Context(), bytes.NewReader(src))
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
	_, _, err := thumbs.Make(t.Context(), bytes.NewReader(huge))
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
		data, ctype, err := thumbs.Make(t.Context(), bytes.NewReader(c.data))
		if err == nil || data != nil || ctype != "" {
			t.Errorf("%s: want an error, got %d bytes %q", name, len(data), ctype)
			continue
		}
		if errors.Is(err, thumbs.ErrUnsupported) != c.unsupported {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// exifJPEG is a w x h JPEG, its left third red and the rest blue, with an
// Exif APP1 segment holding app1 (after "Exif\0\0") right after SOI.
func exifJPEG(t *testing.T, w, h int, tiff []byte) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{0, 0, 255, 255}
			if x < w/3 {
				c = color.NRGBA{255, 0, 0, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	plain := encodeJPEG(t, img)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := append([]byte{0xff, 0xe1}, binary.BigEndian.AppendUint16(nil, uint16(len(seg)+2))...)
	out := append([]byte{}, plain[:2]...)
	out = append(out, append(app1, seg...)...)
	return append(out, plain[2:]...)
}

// orientationTIFF is a big-endian TIFF block whose first IFD holds only
// the orientation tag set to v.
func orientationTIFF(v uint16) []byte {
	b := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01")
	b = binary.BigEndian.AppendUint16(b, v)
	return append(b, 0, 0, 0, 0, 0, 0)
}

func isRed(c color.Color) bool {
	r, _, b, _ := c.RGBA()
	return r > 0xa000 && b < 0x6000
}

// A phone photo stored sideways with an Exif orientation comes out upright.
func TestMakeAppliesExifOrientation(t *testing.T) {
	for _, c := range []struct {
		v            uint16
		w, h         int
		redX, redY   int // a pixel that must be red
		blueX, blueY int // and one that must not be
	}{
		{1, 192, 64, 5, 32, 186, 32},
		{6, 64, 192, 32, 5, 32, 186}, // a quarter clockwise: the left goes to the top
		{8, 64, 192, 32, 186, 32, 5}, // a quarter anticlockwise: the left goes to the bottom
		{3, 192, 64, 186, 32, 5, 32}, // half round: the left goes to the right
		{2, 192, 64, 186, 32, 5, 32}, // mirrored
		{5, 64, 192, 32, 5, 32, 186}, // across the diagonal: the left goes to the top
		{7, 64, 192, 32, 186, 32, 5}, // across the other: the left goes to the bottom
		{4, 192, 64, 5, 32, 186, 32}, // upside down: the left stays left
	} {
		data, _, err := thumbs.Make(t.Context(), bytes.NewReader(exifJPEG(t, 300, 100, orientationTIFF(c.v))))
		if err != nil {
			t.Fatalf("%d: %v", c.v, err)
		}
		img, _ := decode(t, data)
		if b := img.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Fatalf("orientation %d: %dx%d, want %dx%d", c.v, b.Dx(), b.Dy(), c.w, c.h)
		}
		if !isRed(img.At(c.redX, c.redY)) || isRed(img.At(c.blueX, c.blueY)) {
			t.Errorf("orientation %d: not upright", c.v)
		}
	}
}

// Malformed Exif is ignored: the preview is the image as stored.
func TestMakeIgnoresMalformedExif(t *testing.T) {
	good := orientationTIFF(6)
	for name, tiff := range map[string][]byte{
		"empty":         nil,
		"short":         good[:5],
		"bad order":     append([]byte("XX"), good[2:]...),
		"not 42":        append(append([]byte{}, good[:3]...), append([]byte{0x2b}, good[4:]...)...),
		"ifd past end":  append(append([]byte{}, good[:4]...), 0x7f, 0, 0, 0),
		"count too big": append(append([]byte{}, good[:8]...), append([]byte{0xff, 0xff, 0x01, 0x00}, good[12:]...)...), // no orientation before the end
		"cut entry":     good[:16],
		"wrong type":    append(append(append([]byte{}, good[:12]...), 0x00, 0x04), good[14:]...),
		"value too big": append(append(append([]byte{}, good[:18]...), 0x00, 0x09), good[20:]...),
	} {
		data, _, err := thumbs.Make(t.Context(), bytes.NewReader(exifJPEG(t, 300, 100, tiff)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		img, _ := decode(t, data)
		if b := img.Bounds(); b.Dx() != 192 || b.Dy() != 64 {
			t.Errorf("%s: %dx%d", name, b.Dx(), b.Dy())
		}
	}
}

// A JPEG of many scans (each a full pass of decoding) is refused from its
// markers alone, quickly.
func TestMakeRefusesManyScans(t *testing.T) {
	plain := encodeJPEG(t, opaque(64, 64))
	sos := bytes.Index(plain, []byte{0xff, 0xda})
	eoi := len(plain) - 2
	if sos < 0 || !bytes.Equal(plain[eoi:], []byte{0xff, 0xd9}) {
		t.Fatal("unexpected JPEG layout")
	}
	scan := plain[sos:eoi]
	build := func(n int) []byte {
		out := append([]byte{}, plain[:sos]...)
		for range n {
			out = append(out, scan...)
		}
		return append(out, 0xff, 0xd9)
	}
	start := time.Now()
	if _, _, err := thumbs.Make(t.Context(), bytes.NewReader(build(thumbs.MaxScans+1))); !errors.Is(err, thumbs.ErrUnsupported) {
		t.Fatalf("%d scans: %v", thumbs.MaxScans+1, err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("refusing took %v", d)
	}
	// At the limit it is still made (the extra scans repeat the first).
	if _, _, err := thumbs.Make(t.Context(), bytes.NewReader(build(3))); err != nil {
		t.Fatalf("3 scans: %v", err)
	}
}
