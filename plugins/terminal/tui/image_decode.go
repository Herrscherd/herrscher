package tui

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"

	_ "image/gif"  // registers the GIF decoder with image.Decode
	_ "image/jpeg" // registers the JPEG decoder with image.Decode

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers the WebP decoder with image.Decode
)

// image.go used to hold three decisions at once: which formats are previewable,
// how big a source may be, and how to speak the kitty protocol. Only the last is
// about kitty. This file owns the first two, so a second encoder (sixel,
// half-blocks) gets the same pixels without reimplementing any of it.

// maxDecodeBytes bounds the *source* before it is decoded, so a hostile or
// merely enormous file cannot make the TUI allocate a canvas out of proportion
// to the ten rows it will end up occupying. It is deliberately generous: it
// protects the decode, not the screen. What protects the screen is
// maxPreviewBytes, which now applies after downscaling.
const maxDecodeBytes = 8 << 20

// maxDecodePixels bounds the *canvas* the decoder is allowed to allocate, which
// maxDecodeBytes cannot: compression means the source size and the pixel count
// are unrelated, and the smallest file that reaches this package can declare the
// largest image. 64 megapixels is well past any screenshot and about 256 MiB of
// RGBA — the point at which drawing a ten-row thumbnail stops being worth it.
const maxDecodePixels = 64 << 20

// cellPixels is the assumed height of a terminal cell in pixels. The terminal
// knows the real number and will not say without a round trip on stdin (see
// capabilities.go), so this is a working figure: being wrong makes the preview
// slightly taller or shorter than previewRows, never unbounded.
const cellPixels = 20

// decodeImage decodes PNG, JPEG, GIF or WebP bytes. It returns an error rather
// than panicking on anything it cannot read — a mislabelled attachment and a
// truncated download are ordinary events, and neither is a reason to lose a turn.
func decodeImage(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, errors.New("image is empty")
	}
	if len(data) > maxDecodeBytes {
		return nil, fmt.Errorf("image is %d bytes, past the %d-byte decode bound", len(data), maxDecodeBytes)
	}
	// The header is read first because the byte count says nothing about the
	// canvas: every format here compresses, so a few kilobytes can declare a
	// hundred-megapixel image and the decoder would allocate all of it before
	// returning. The bound is on what gets allocated, not on what arrived.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read image header: %w", err)
	}
	if px := int64(cfg.Width) * int64(cfg.Height); px > maxDecodePixels {
		return nil, fmt.Errorf("image is %dx%d, past the %d-pixel decode bound", cfg.Width, cfg.Height, maxDecodePixels)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

// downscale shrinks img so it occupies at most maxRows terminal rows, preserving
// the aspect ratio. An image already small enough is returned untouched:
// resampling it would cost a pass and hand back a blurrier copy of the same
// picture.
func downscale(img image.Image, maxRows int) image.Image {
	if maxRows < 1 {
		maxRows = 1
	}
	b := img.Bounds()
	maxHeight := maxRows * cellPixels
	if b.Dy() <= maxHeight {
		return img
	}
	w := b.Dx() * maxHeight / b.Dy()
	if w < 1 {
		w = 1
	}
	out := image.NewRGBA(image.Rect(0, 0, w, maxHeight))
	// ApproxBiLinear over CatmullRom: at these sizes the difference is invisible
	// and the cost is not — this runs on the Update goroutine, between keystrokes.
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), img, b, xdraw.Src, nil)
	return out
}

// encodePNG re-encodes an image as PNG, which is what the kitty transmission
// (f=100) consumes. Every source format therefore reaches kitty, instead of PNG
// passing and the rest being dropped.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}
