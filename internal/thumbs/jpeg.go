package thumbs

import (
	"encoding/binary"
	"image"
)

// scanJPEG walks a JPEG's markers once and returns how many scans (SOS
// markers) it has and its Exif orientation (1 when there is none or it
// can't be read). It never reads past src: a damaged file stops the walk
// with what was counted so far, and the decoder reports the damage.
func scanJPEG(src []byte) (scans, orientation int) {
	orientation = 1
	exifSeen := false
	i := 2 // after SOI
	for i+1 < len(src) {
		if src[i] != 0xff {
			return scans, orientation
		}
		m := src[i+1]
		i += 2
		switch {
		case m == 0xff: // fill byte: the marker follows
			i--
			continue
		case m == 0xd9: // EOI
			return scans, orientation
		case m == 0x01 || m == 0xd8 || (m >= 0xd0 && m <= 0xd7): // no length
			continue
		}
		if i+2 > len(src) {
			return scans, orientation
		}
		n := int(binary.BigEndian.Uint16(src[i:]))
		if n < 2 || i+n > len(src) {
			return scans, orientation
		}
		seg := src[i+2 : i+n]
		i += n
		switch m {
		case 0xe1: // APP1: the first Exif one counts
			if !exifSeen && len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
				exifSeen = true
				orientation = exifOrientation(seg[6:])
			}
		case 0xda: // SOS: entropy-coded data follows, up to the next marker
			scans++
			if scans > MaxScans {
				return scans, orientation
			}
			for i+1 < len(src) && (src[i] != 0xff || src[i+1] == 0x00 || (src[i+1] >= 0xd0 && src[i+1] <= 0xd7)) {
				i++
			}
		}
	}
	return scans, orientation
}

// exifOrientation reads tag 0x0112 from the first IFD of a TIFF block (an
// Exif APP1 segment after its "Exif\0\0"). Anything malformed, or a value
// outside 1 to 8, is 1.
func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(tiff[2:]) != 42 {
		return 1
	}
	off := int64(bo.Uint32(tiff[4:]))
	if off < 8 || off+2 > int64(len(tiff)) {
		return 1
	}
	count := int64(bo.Uint16(tiff[off:]))
	for k := range count {
		e := off + 2 + k*12
		if e+12 > int64(len(tiff)) {
			return 1
		}
		if bo.Uint16(tiff[e:]) != 0x0112 {
			continue
		}
		// SHORT (type 3), one value, held in the entry itself.
		if bo.Uint16(tiff[e+2:]) != 3 || bo.Uint32(tiff[e+4:]) != 1 {
			return 1
		}
		if v := int(bo.Uint16(tiff[e+8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// orient turns img upright for an Exif orientation: 2 mirrors it, 3 turns
// it half round, 4 flips it upside down, 5 mirrors it across the top-left
// diagonal, 6 turns it a quarter clockwise, 7 mirrors it across the other
// diagonal, 8 turns it a quarter anticlockwise. 1 (or anything else)
// leaves it alone.
func orient(img *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			si, di := img.PixOffset(x, y), out.PixOffset(dx, dy)
			copy(out.Pix[di:di+4], img.Pix[si:si+4])
		}
	}
	return out
}
