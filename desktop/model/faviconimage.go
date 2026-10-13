package model

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
)

// A site's icon as the CLI found it is a PNG, JPEG, GIF, WebP, or an ICO file, which not every
// platform's image decoder reads. FaviconImage turns an ICO into the PNG of its best image and
// leaves the others as they are.

var errIcon = errors.New("not an icon this app can draw")

// preferredIconSide is the side of the image an ICO is read at: big enough for a chip's icon on a
// 2x display.
const preferredIconSide = 32

// FaviconImage is the image bytes to decode for an icon the CLI sent.
func FaviconImage(data []byte) ([]byte, error) {
	if len(data) < 6 || data[0] != 0 || data[1] != 0 || data[2] != 1 || data[3] != 0 {
		return data, nil
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	type entry struct{ side, offset, size int }
	var best *entry
	for i := 0; i < count && 6+16*(i+1) <= len(data); i++ {
		e := data[6+16*i : 6+16*(i+1)]
		side := int(e[0])
		if side == 0 {
			side = 256
		}
		candidate := entry{side: side, size: int(binary.LittleEndian.Uint32(e[8:12])), offset: int(binary.LittleEndian.Uint32(e[12:16]))}
		if candidate.offset < 0 || candidate.size <= 0 || candidate.offset+candidate.size > len(data) {
			continue
		}
		// The smallest image at least the preferred side, else the largest there is.
		switch {
		case best == nil:
			best = &candidate
		case candidate.side >= preferredIconSide && (best.side < preferredIconSide || candidate.side < best.side):
			best = &candidate
		case candidate.side < preferredIconSide && best.side < preferredIconSide && candidate.side > best.side:
			best = &candidate
		}
	}
	if best == nil {
		return nil, errIcon
	}
	body := data[best.offset : best.offset+best.size]
	if bytes.HasPrefix(body, []byte("\x89PNG")) {
		return body, nil
	}
	return dibToPNG(body)
}

// dibToPNG reads the bitmap an ICO entry holds, without its file header: 24 or 32 bits a pixel,
// bottom row first, with a mask of transparent pixels after a 24-bit image.
func dibToPNG(body []byte) ([]byte, error) {
	if len(body) < 40 || binary.LittleEndian.Uint32(body[0:4]) < 40 {
		return nil, errIcon
	}
	header := int(binary.LittleEndian.Uint32(body[0:4]))
	width := int(int32(binary.LittleEndian.Uint32(body[4:8])))
	// The height counts the mask too.
	height := int(int32(binary.LittleEndian.Uint32(body[8:12]))) / 2
	bits := int(binary.LittleEndian.Uint16(body[14:16]))
	compression := binary.LittleEndian.Uint32(body[16:20])
	if width <= 0 || height <= 0 || width > 256 || height > 256 || (bits != 32 && bits != 24) || (compression != 0 && compression != 3) {
		return nil, errIcon
	}
	// The site writes the header's size: one past the entry is a broken icon, not a slice to take.
	if header > len(body) {
		return nil, errIcon
	}
	pixels := body[header:]
	stride := (width*bits/8 + 3) &^ 3
	if len(pixels) < stride*height {
		return nil, errIcon
	}
	mask := pixels[stride*height:]
	maskStride := ((width + 31) / 32) * 4
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	opaqueSeen := false
	for y := 0; y < height; y++ {
		row := pixels[(height-1-y)*stride:]
		for x := 0; x < width; x++ {
			b, g, r := row[x*bits/8], row[x*bits/8+1], row[x*bits/8+2]
			a := byte(255)
			if bits == 32 {
				a = row[x*4+3]
				opaqueSeen = opaqueSeen || a != 0
			} else if len(mask) >= maskStride*height && mask[(height-1-y)*maskStride+x/8]&(0x80>>(x%8)) != 0 {
				a = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	// A 32-bit image whose alpha is all zero is an opaque one that left the channel unused.
	if bits == 32 && !opaqueSeen {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
