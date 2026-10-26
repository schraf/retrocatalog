package main

import (
	"bytes"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"
)

const thumbQuality = 82

// thumbBackground matches the panel background and shows through transparent art.
var thumbBackground = color.RGBA{R: 0x14, G: 0x16, B: 0x1c, A: 0xff}

// encodeDownscaled resizes img to maxWidth using an area-average (box) filter
// and encodes the result as JPEG. Images already narrower than maxWidth keep
// their size. Transparent pixels are flattened onto the panel background.
func encodeDownscaled(img image.Image, maxWidth int) ([]byte, error) {
	dst := downscale(img, maxWidth)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: thumbQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func downscale(img image.Image, maxWidth int) *image.RGBA {
	bounds := img.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	if sw <= 0 || sh <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}

	dw, dh := sw, sh
	if maxWidth > 0 && sw > maxWidth {
		dw = maxWidth
		dh = int(math.Round(float64(sh) * float64(maxWidth) / float64(sw)))
		if dh < 1 {
			dh = 1
		}
	}

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	bgR := uint64(thumbBackground.R) * 0x101
	bgG := uint64(thumbBackground.G) * 0x101
	bgB := uint64(thumbBackground.B) * 0x101

	xs := make([]int, dw+1)
	for x := range xs {
		xs[x] = bounds.Min.X + x*sw/dw
	}
	ys := make([]int, dh+1)
	for y := range ys {
		ys[y] = bounds.Min.Y + y*sh/dh
	}

	if src, ok := img.(*image.YCbCr); ok {
		for y := 0; y < dh; y++ {
			y0, y1 := ys[y], clampMin(ys[y+1], ys[y]+1)
			for x := 0; x < dw; x++ {
				x0, x1 := xs[x], clampMin(xs[x+1], xs[x]+1)

				var sumY, sumCb, sumCr, count uint64
				for sy := y0; sy < y1; sy++ {
					for sx := x0; sx < x1; sx++ {
						c := src.YCbCrAt(sx, sy)
						sumY += uint64(c.Y)
						sumCb += uint64(c.Cb)
						sumCr += uint64(c.Cr)
						count++
					}
				}

				r, g, b := color.YCbCrToRGB(
					uint8(sumY/count),
					uint8(sumCb/count),
					uint8(sumCr/count),
				)
				setRGB(dst, x, y, r, g, b)
			}
		}
		return dst
	}

	for y := 0; y < dh; y++ {
		y0, y1 := ys[y], clampMin(ys[y+1], ys[y]+1)
		for x := 0; x < dw; x++ {
			x0, x1 := xs[x], clampMin(xs[x+1], xs[x]+1)

			// RGBA returns alpha-premultiplied 16-bit components.
			var sumR, sumG, sumB, sumA, count uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, b, a := img.At(sx, sy).RGBA()
					sumR += uint64(r)
					sumG += uint64(g)
					sumB += uint64(b)
					sumA += uint64(a)
					count++
				}
			}

			r := sumR / count
			g := sumG / count
			b := sumB / count
			a := sumA / count

			inv := uint64(0xffff) - a
			r += (bgR * inv) / 0xffff
			g += (bgG * inv) / 0xffff
			b += (bgB * inv) / 0xffff

			setRGB(dst, x, y, uint8(r>>8), uint8(g>>8), uint8(b>>8))
		}
	}
	return dst
}

func clampMin(value, min int) int {
	if value < min {
		return min
	}
	return value
}

func setRGB(dst *image.RGBA, x, y int, r, g, b uint8) {
	i := dst.PixOffset(x, y)
	dst.Pix[i+0] = r
	dst.Pix[i+1] = g
	dst.Pix[i+2] = b
	dst.Pix[i+3] = 0xff
}
