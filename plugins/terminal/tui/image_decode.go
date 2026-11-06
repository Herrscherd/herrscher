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

var (
	errEmptyImage    = errors.New("image.empty")
	errImageTooLarge = errors.New("image.too_large")
)

const maxDecodeBytes = 8 << 20

const maxDecodePixels = 64 << 20

const cellPixels = 20

func decodeImage(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, errEmptyImage
	}
	if len(data) > maxDecodeBytes {
		return nil, fmt.Errorf("%w: %d/%d", errImageTooLarge, len(data), maxDecodeBytes)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read image header: %w", err)
	}
	if px := int64(cfg.Width) * int64(cfg.Height); px > maxDecodePixels {
		return nil, fmt.Errorf("%w: %dx%d/%d", errImageTooLarge, cfg.Width, cfg.Height, maxDecodePixels)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

const maxPreviewWidth = 2000

func downscale(img image.Image) image.Image {
	bounds := img.Bounds()
	maxHeight := previewRows * cellPixels
	if bounds.Dy() <= maxHeight && bounds.Dx() <= maxPreviewWidth {
		return img
	}
	height, width := bounds.Dy(), bounds.Dx()
	if height > maxHeight {
		height, width = maxHeight, max(1, bounds.Dx()*maxHeight/bounds.Dy())
	}
	if width > maxPreviewWidth {
		height, width = max(1, height*maxPreviewWidth/width), maxPreviewWidth
	}
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), img, bounds, xdraw.Src, nil)
	return out
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}
