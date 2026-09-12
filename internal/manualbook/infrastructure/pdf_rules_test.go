package infrastructure

import (
	"image"
	"image/color"
	"math/rand/v2"
	"reflect"
	"testing"
)

// RGBA の型判定だけを避け、従来の draw.Draw と同じ RGBA64 経路で比較する。
type genericImage struct{ image.RGBA64Image }

func TestImageRulesRGBA(t *testing.T) {
	img := image.NewRGBA(image.Rect(-3, 5, 157, 145))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.Uint32())
	}
	// 短い画だけでなく、閾値の前後の水平線・垂直線と画像端も比較する。
	for y := 10; y < 140; y++ {
		for x := 0; x < 150; x++ {
			c := uint8(179 + (y/10)%2)
			if x < 10 || x >= 140 {
				c = 0
			}
			img.SetRGBA(x, y, color.RGBA{c, c, c, 255})
		}
	}
	colors := image.NewRGBA(image.Rect(0, 0, 160, 256))
	for y := 0; y < 256; y++ {
		c := color.RGBA{uint8(rng.Uint32()), uint8(rng.Uint32()), uint8(rng.Uint32()), uint8(rng.Uint32())}
		for x := 0; x < 160; x++ {
			colors.SetRGBA(x, y, c)
		}
	}
	for _, tc := range []struct {
		name string
		img  *image.RGBA
	}{
		{"full", img},
		{"subimage", img.SubImage(image.Rect(1, 11, 148, 139)).(*image.RGBA)},
		{"colors", colors},
		{"empty", image.NewRGBA(image.Rectangle{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := imageRules(tc.img, 80, 70)
			want := imageRules(genericImage{tc.img}, 80, 70)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("RGBA rules differ from standard conversion: got %d, want %d", len(got), len(want))
			}
		})
	}
}

func BenchmarkImageRules(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1190, 1684))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y := 20; y < 1684; y += 40 {
		for x := 20; x < 1170; x++ {
			img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
		}
	}
	for _, tc := range []struct {
		name string
		img  image.Image
	}{{"RGBA", img}, {"generic", genericImage{img}}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				imageRules(tc.img, 595, 842)
			}
		})
	}
}
