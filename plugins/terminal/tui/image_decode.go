package tui

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"

	_ "image/gif"
	_ "image/jpeg"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const maxDecodeBytes = 8 << 20

const maxDecodePixels = 64 << 20

const cellPixels = 20

func decodeImage(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, errors.New("image is empty")
	}
	if len(data) > maxDecodeBytes {
		return nil, fmt.Errorf("image is %d bytes, past the %d-byte decode bound", len(data), maxDecodeBytes)
	}
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

const maxPreviewWidth = 2000

func downscale(img image.Image, maxRows int) image.Image {
	if maxRows < 1 {
		maxRows = 1
	}
	b := img.Bounds()
	maxHeight := maxRows * cellPixels
	if b.Dy() <= maxHeight && b.Dx() <= maxPreviewWidth {
		return img
	}
	h, w := b.Dy(), b.Dx()
	if h > maxHeight {
		h, w = maxHeight, max(1, b.Dx()*maxHeight/b.Dy())
	}
	if w > maxPreviewWidth {
		h, w = max(1, h*maxPreviewWidth/w), maxPreviewWidth
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), img, b, xdraw.Src, nil)
	return out
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}
