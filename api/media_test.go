package api

import (
	"math"
	"testing"
)

func TestMediaValidationRejectsUnencodableOrInvalidOptions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		m       MediaOptions
		sticker bool
	}{
		{"nonfinite start", MediaOptions{Start: math.NaN()}, false},
		{"nonfinite end", MediaOptions{End: math.Inf(1)}, false},
		{"reverse clip", MediaOptions{Start: 5, End: 2}, false},
		{"partial dimensions", MediaOptions{Width: 640}, false},
		{"odd H264 width", MediaOptions{Width: 641, Height: 480}, false},
		{"oversize GIF", MediaOptions{Width: 2050, Height: 480}, false},
		{"invalid sticker long edge", MediaOptions{Width: 256, Height: 256}, true},
		{"sticker frame limit", MediaOptions{FPS: 31}, true},
		{"filter injection", MediaOptions{Resize: "crop,scale=100:100"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.m.ValidateMedia(tc.sticker); err == nil {
				t.Fatal("invalid conversion accepted")
			}
		})
	}
	if err := (MediaOptions{Start: 5, End: 20, Width: 1280, Height: 720, Resize: "pad", FPS: 24, Quality: "high"}).ValidateMedia(false); err != nil {
		t.Fatalf("valid long GIF settings rejected: %v", err)
	}
}
