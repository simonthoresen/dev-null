package main

import (
	"image/color"
	"testing"
)

func TestLauncherSceneRendersVisiblePixels(t *testing.T) {
	scene := newLauncherScene()
	img := scene.Render(320, 200, 1.5)
	if img == nil {
		t.Fatal("expected launcher scene image, got nil")
	}

	bg := color.RGBA{R: 0x03, G: 0x05, B: 0x0d, A: 0xff}
	nonBackground := 0
	brightPixels := 0

	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if c != bg {
				nonBackground++
			}
			if c.R > 180 || c.G > 180 || c.B > 180 {
				brightPixels++
			}
		}
	}

	if nonBackground < 500 {
		t.Fatalf("expected scene details to draw, non-background pixel count=%d", nonBackground)
	}
	if brightPixels < 40 {
		t.Fatalf("expected stars/highlights to render, bright pixel count=%d", brightPixels)
	}
}

func TestLauncherSceneReusesBackingImage(t *testing.T) {
	scene := newLauncherScene()
	a := scene.Render(160, 100, 0)
	b := scene.Render(160, 100, 1)
	if a != b {
		t.Fatal("expected same backing image when dimensions are unchanged")
	}
	c := scene.Render(200, 100, 1)
	if c == b {
		t.Fatal("expected new backing image after size change")
	}
}
