package model

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"testing"
)

// The icons sites really serve: ICO files with PNG or bitmap images inside.
func TestFaviconImageReadsRealIcons(t *testing.T) {
	for _, name := range []string{"docs.rs", "github.com"} {
		data, err := os.ReadFile("testdata/" + name + ".ico")
		if err != nil {
			t.Fatal(err)
		}
		image, err := FaviconImage(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		decoded, err := png.Decode(bytes.NewReader(image))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if side := decoded.Bounds().Dx(); side < 16 || side > 64 || decoded.Bounds().Dy() != side {
			t.Errorf("%s: %v", name, decoded.Bounds())
		}
		var seen bool
		for y := 0; y < decoded.Bounds().Dy() && !seen; y++ {
			for x := 0; x < decoded.Bounds().Dx(); x++ {
				if _, _, _, a := decoded.At(x, y).RGBA(); a != 0 {
					seen = true
					break
				}
			}
		}
		if !seen {
			t.Errorf("%s: nothing drawn", name)
		}
	}
}

func TestFaviconImageLeavesOtherFormatsAlone(t *testing.T) {
	for _, data := range [][]byte{[]byte("\x89PNG\r\n\x1a\nrest"), []byte("\xff\xd8\xff\xe0jpeg"), []byte("GIF89a...")} {
		got, err := FaviconImage(data)
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("%q: %q, %v", data[:4], got, err)
		}
	}
}

func TestFaviconImageTakesTheSmallestAtLeastThirtyTwo(t *testing.T) {
	// Three 24-bit bitmaps, 16, 48, and 32 on a side, each filled with its side as the red value.
	var entries, images bytes.Buffer
	sides := []int{16, 48, 32}
	offset := 6 + 16*len(sides)
	for _, side := range sides {
		stride := (side*3 + 3) &^ 3
		maskStride := ((side + 31) / 32) * 4
		var dib bytes.Buffer
		for _, field := range []any{uint32(40), int32(side), int32(2 * side), uint16(1), uint16(24), uint32(0), uint32(0), int32(0), int32(0), uint32(0), uint32(0)} {
			binary.Write(&dib, binary.LittleEndian, field)
		}
		pixels := make([]byte, stride*side)
		for i := 0; i < side; i++ {
			for x := 0; x < side; x++ {
				pixels[i*stride+x*3+2] = byte(side)
			}
		}
		dib.Write(pixels)
		dib.Write(make([]byte, maskStride*side))
		entries.Write([]byte{byte(side), byte(side), 0, 0, 1, 0, 24, 0})
		binary.Write(&entries, binary.LittleEndian, []uint32{uint32(dib.Len()), uint32(offset + images.Len())})
		images.Write(dib.Bytes())
	}
	file := append([]byte{0, 0, 1, 0, byte(len(sides)), 0}, append(entries.Bytes(), images.Bytes()...)...)
	got, err := FaviconImage(file)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _, _ := decoded.At(3, 3).RGBA(); decoded.Bounds().Dx() != 32 || r>>8 != 32 {
		t.Errorf("took a %d image with red %d", decoded.Bounds().Dx(), r>>8)
	}
}
