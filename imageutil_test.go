package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestEncodeDownscaled(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1530, 2100))
	for y := 0; y < 2100; y++ {
		for x := 0; x < 1530; x++ {
			c := color.NRGBA{R: 220, G: 30, B: 40, A: 255}
			if x >= 765 {
				c = color.NRGBA{R: 30, G: 60, B: 210, A: 255}
			}
			src.SetNRGBA(x, y, c)
		}
	}

	data, err := encodeDownscaled(src, gridThumbWidth)
	if err != nil {
		t.Fatal(err)
	}

	out, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" {
		t.Fatalf("format = %q, want jpeg", format)
	}
	if got := out.Bounds().Dx(); got != gridThumbWidth {
		t.Fatalf("width = %d, want %d", got, gridThumbWidth)
	}
	if got, want := out.Bounds().Dy(), 494; got != want {
		t.Fatalf("height = %d, want %d", got, want)
	}

	left := color.RGBAModel.Convert(out.At(40, 240)).(color.RGBA)
	if left.R < 170 || left.B > 90 {
		t.Fatalf("left pixel = %#v, want red-ish", left)
	}
	right := color.RGBAModel.Convert(out.At(320, 240)).(color.RGBA)
	if right.B < 150 || right.R > 100 {
		t.Fatalf("right pixel = %#v, want blue-ish", right)
	}
}

func TestEncodeDownscaledDoesNotUpscale(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 120, 80))
	data, err := encodeDownscaled(src, panelThumbWidth)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if out.Bounds().Dx() != 120 || out.Bounds().Dy() != 80 {
		t.Fatalf("size = %dx%d, want 120x80", out.Bounds().Dx(), out.Bounds().Dy())
	}
}

func TestEncodeDownscaledFlattensTransparency(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 200, 300))
	data, err := encodeDownscaled(src, 100)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	got := color.RGBAModel.Convert(out.At(50, 75)).(color.RGBA)
	if diff(got.R, thumbBackground.R) > 12 || diff(got.G, thumbBackground.G) > 12 || diff(got.B, thumbBackground.B) > 12 {
		t.Fatalf("transparent pixel = %#v, want near %#v", got, thumbBackground)
	}
}

func TestJPEGSourceUsesFastPath(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 800, 1200))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img.(*image.YCbCr); !ok {
		t.Fatalf("decoded JPEG is %T, want *image.YCbCr", img)
	}

	data, err := encodeDownscaled(img, gridThumbWidth)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if out.Bounds().Dx() != gridThumbWidth || out.Bounds().Dy() != 540 {
		t.Fatalf("size = %dx%d, want 360x540", out.Bounds().Dx(), out.Bounds().Dy())
	}
}

func diff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
