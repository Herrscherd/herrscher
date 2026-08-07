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

// The fixtures are encoded in memory rather than committed: a binary blob in the
// tree is a thing nobody can review, and the encoders that produce them are the
// same ones the decoder has to read.
func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
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

// Corrupt bytes are an ordinary outcome — an attachment mislabelled, a truncated
// download — and must come back as an error, never as a panic that takes the turn
// with it.
func TestDecodeImageRefusesCorruptBytes(t *testing.T) {
	if _, err := decodeImage([]byte("not an image at all")); err == nil {
		t.Fatal("corrupt bytes must return an error")
	}
	if _, err := decodeImage(nil); err == nil {
		t.Fatal("empty bytes must return an error")
	}
}

// maxDecodeBytes bounds the bytes that arrived, and is checked before any of
// them are parsed.
func TestDecodeImageRefusesAnOversizedSource(t *testing.T) {
	if _, err := decodeImage(bytes.Repeat([]byte("x"), maxDecodeBytes+1)); err == nil {
		t.Fatal("a source past maxDecodeBytes must be refused")
	}
}

// The byte bound is not the pixel bound: every format here compresses, so the
// smallest file that reaches this package can declare the largest canvas. A
// header claiming a gigapixel image must be refused from the header, before the
// decoder allocates for it.
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

// pngHeaderOnly is a PNG signature plus one IHDR chunk, which is all
// image.DecodeConfig reads. There is deliberately no image data behind it: the
// point is that the dimensions alone are enough to refuse.
func pngHeaderOnly(w, h uint32) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	binary.Write(&ihdr, binary.BigEndian, w)
	binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 6, 0, 0, 0}) // 8-bit RGBA, no interlace

	var out bytes.Buffer
	out.Write([]byte("\x89PNG\r\n\x1a\n"))
	binary.Write(&out, binary.BigEndian, uint32(ihdr.Len()-4)) // length excludes the type
	out.Write(ihdr.Bytes())
	binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}

// The cap that used to sit on the source file rejected a photo that downscales
// to a few kilobytes. It now sits on the encoded payload, which is the thing the
// viewport actually re-scans on every repaint.
func TestDownscaleBringsALargeImageUnderThePreviewCap(t *testing.T) {
	// Noise, from a fixed seed so the fixture is the same every run: PNG cannot
	// compress it away, so at full size it is well past maxPreviewBytes — which
	// is exactly the photo the old source-side cap used to reject outright.
	rng := rand.New(rand.NewSource(1))
	big := image.NewRGBA(image.Rect(0, 0, 900, 700))
	for y := 0; y < 700; y++ {
		for x := 0; x < 900; x++ {
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

// An image already small enough is left exactly as it is: rescaling it would
// cost a resample and give back a blurrier version of the same picture.
func TestDownscaleLeavesASmallImageAlone(t *testing.T) {
	src := testImage(8, 8)
	if got := downscale(src, previewRows); got != src {
		t.Fatalf("a small image must be returned untouched")
	}
}

// The row cap does not bound the width, and a very wide banner is already short
// enough to keep every one of its pixels. Each encoder then pays for all of them
// — sixel walks its palette across the full width once per band — for a picture
// no terminal is wide enough to show.
func TestDownscaleBoundsAWideImageToo(t *testing.T) {
	wide := image.NewRGBA(image.Rect(0, 0, 20000, 40)) // short: the row cap does not fire
	got := downscale(wide, previewRows).Bounds()
	if got.Dx() > maxPreviewWidth {
		t.Fatalf("a wide image must be bounded on width too, got %v", got)
	}
	if got.Dy() < 1 {
		t.Fatalf("the aspect ratio must not collapse the image away: %v", got)
	}
}
