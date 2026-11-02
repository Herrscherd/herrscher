package tui

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"strings"
	"testing"
)

func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x40, A: 0xff})
		}
	}
	return img
}

func encodeFixture(t *testing.T, format string, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatalf("encode %s fixture: %v", format, err)
	}
	return buf.Bytes()
}

func TestDecodeImageReadsEveryFormat(t *testing.T) {
	src := testImage(16, 9)
	for _, format := range []string{"png", "jpeg", "gif"} {
		t.Run(format, func(t *testing.T) {
			got, err := decodeImage(encodeFixture(t, format, src))
			if err != nil {
				t.Fatalf("decodeImage(%s) = %v", format, err)
			}
			if b := got.Bounds(); b.Dx() != 16 || b.Dy() != 9 {
				t.Fatalf("decoded bounds = %v, want 16x9", b)
			}
		})
	}
}

func TestDecodeImageRefusesCorruptBytes(t *testing.T) {
	if _, err := decodeImage([]byte("not an image at all")); err == nil {
		t.Fatal("corrupt bytes must return an error")
	}
	if _, err := decodeImage(nil); err == nil {
		t.Fatal("empty bytes must return an error")
	}
}

func TestDecodeImageRefusesAnOversizedSource(t *testing.T) {
	if _, err := decodeImage(bytes.Repeat([]byte("x"), maxDecodeBytes+1)); err == nil {
		t.Fatal("a source past maxDecodeBytes must be refused")
	}
}

func TestDecodeImageRefusesAGigapixelHeader(t *testing.T) {
	data := pngHeaderOnly(40000, 40000)
	if int64(40000)*40000 <= maxDecodePixels {
		t.Fatal("the fixture must be past maxDecodePixels")
	}
	if len(data) > 1024 {
		t.Fatalf("the fixture must be tiny to make the point: %d bytes", len(data))
	}
	_, err := decodeImage(data)
	if err == nil {
		t.Fatal("a header past maxDecodePixels must be refused")
	}
	if !strings.Contains(err.Error(), "pixel") {
		t.Fatalf("the refusal must name the pixel bound: %v", err)
	}
}

func pngHeaderOnly(w, h uint32) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	ihdr.Write(binary.BigEndian.AppendUint32(nil, w))
	ihdr.Write(binary.BigEndian.AppendUint32(nil, h))
	ihdr.Write([]byte{8, 6, 0, 0, 0})

	var out bytes.Buffer
	out.Write([]byte("\x89PNG\r\n\x1a\n"))
	out.Write(binary.BigEndian.AppendUint32(nil, uint32(ihdr.Len()-4)))
	out.Write(ihdr.Bytes())
	out.Write(binary.BigEndian.AppendUint32(nil, crc32.ChecksumIEEE(ihdr.Bytes())))
	return out.Bytes()
}

func TestDownscaleBringsALargeImageUnderThePreviewCap(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	big := image.NewRGBA(image.Rect(0, 0, 900, 700))
	for y := range 700 {
		for x := range 900 {
			big.Set(x, y, color.RGBA{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256)), A: 0xff})
		}
	}
	source, err := encodePNG(big)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) <= maxPreviewBytes {
		t.Fatalf("fixture is not large enough to exercise the cap (%d bytes)", len(source))
	}

	small := downscale(big, previewRows)
	if small.Bounds().Dy() >= big.Bounds().Dy() {
		t.Fatalf("downscale left the image at %v", small.Bounds())
	}
	encoded, err := encodePNG(small)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxPreviewBytes {
		t.Fatalf("downscaled payload is %d bytes, past the %d cap", len(encoded), maxPreviewBytes)
	}
}

func TestDownscaleLeavesASmallImageAlone(t *testing.T) {
	src := testImage(8, 8)
	if got := downscale(src, previewRows); got != src {
		t.Fatalf("a small image must be returned untouched")
	}
}

func TestDownscaleBoundsAWideImageToo(t *testing.T) {
	wide := image.NewRGBA(image.Rect(0, 0, 20000, 40))
	got := downscale(wide, previewRows).Bounds()
	if got.Dx() > maxPreviewWidth {
		t.Fatalf("a wide image must be bounded on width too, got %v", got)
	}
	if got.Dy() < 1 {
		t.Fatalf("the aspect ratio must not collapse the image away: %v", got)
	}
}
