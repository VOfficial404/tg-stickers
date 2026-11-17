package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tool(t *testing.T) Tool {
	t.Helper()
	ff, err := Find("")
	if err != nil {
		t.Skip(err)
	}
	return ff
}

// makeGIF 用 ffmpeg 自带的测试画面做一个 w×h、secs 秒、带透明的 GIF
func makeGIF(t *testing.T, ff Tool, dir string, w, h int, secs float64) string {
	t.Helper()
	out := filepath.Join(dir, "in.gif")
	src := fmt.Sprintf("testsrc2=size=%dx%d:rate=25:duration=%s", w, h, fmtFloat(secs))
	if err := ff.run(context.Background(), "-f", "lavfi", "-i", src, "-vf", "format=rgba,colorkey=black", out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestVideoStickerMeetsTelegramLimits(t *testing.T) {
	ff := tool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, tc := range []struct {
		name string
		w, h int
		secs float64
		fit  string
	}{
		{"wide long sped up", 640, 360, 5, "speed"},
		{"tall long trimmed", 200, 300, 5, "trim"},
		{"small short upscaled", 100, 80, 1.5, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			in := makeGIF(t, ff, dir, tc.w, tc.h, tc.secs)
			st, err := ff.Sticker(ctx, in, dir, tc.fit)
			if err != nil {
				t.Fatal(err)
			}
			if st.Mime != "video/webm" {
				t.Fatalf("mime = %s, want video/webm", st.Mime)
			}
			fi, err := os.Stat(st.Path)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Size() > MaxVideoBytes {
				t.Errorf("size = %d, over %d", fi.Size(), MaxVideoBytes)
			}
			info, err := ff.Probe(ctx, st.Path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Codec != "vp9" {
				t.Errorf("codec = %s, want vp9", info.Codec)
			}
			if max(info.W, info.H) != StickerSide || min(info.W, info.H) > StickerSide {
				t.Errorf("size = %dx%d, want long side %d", info.W, info.H, StickerSide)
			}
			if info.Duration > MaxVideoSeconds {
				t.Errorf("duration = %.3f, over %v", info.Duration, MaxVideoSeconds)
			}
			if info.FPS > MaxVideoFPS+0.01 {
				t.Errorf("fps = %.2f, over %d", info.FPS, MaxVideoFPS)
			}
		})
	}
}

func TestStaticSticker(t *testing.T) {
	ff := tool(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	if err := ff.run(context.Background(), "-f", "lavfi", "-i", "testsrc2=size=300x200", "-frames:v", "1", in); err != nil {
		t.Fatal(err)
	}
	st, err := ff.Sticker(context.Background(), in, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ff.Probe(context.Background(), st.Path)
	if err != nil {
		t.Fatal(err)
	}
	// 300×200 放大到长边 512，短边 341.3 取偶数
	if info.W != StickerSide || info.H < 340 || info.H > 342 || info.H%2 != 0 {
		t.Errorf("size = %dx%d, want 512x340 or 512x342", info.W, info.H)
	}
}

func TestGIFIsSilentMP4(t *testing.T) {
	ff := tool(t)
	dir := t.TempDir()
	in := makeGIF(t, ff, dir, 1600, 900, 1)
	g, err := ff.GIF(context.Background(), in, dir)
	if err != nil {
		t.Fatal(err)
	}
	if g.W != 1280 || g.H != 720 {
		t.Errorf("size = %dx%d, want 1280x720", g.W, g.H)
	}
	info, err := ff.Probe(context.Background(), g.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec != "h264" {
		t.Errorf("codec = %s, want h264", info.Codec)
	}
}

func TestParseRate(t *testing.T) {
	for in, want := range map[string]float64{"30000/1001": 29.97002997002997, "25/1": 25, "0/0": 0, "": 0, "12.5": 12.5} {
		if got := parseRate(in); got != want {
			t.Errorf("parseRate(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestEvenFit(t *testing.T) {
	for _, tc := range []struct{ w, h, side, ww, wh int }{
		{1920, 1080, 1280, 1280, 720},
		{1081, 1921, 1280, 720, 1280},
		{333, 201, 1280, 332, 200},
		{1, 1, 1280, 2, 2},
	} {
		if w, h := evenFit(tc.w, tc.h, tc.side); w != tc.ww || h != tc.wh {
			t.Errorf("evenFit(%d, %d) = %dx%d, want %dx%d", tc.w, tc.h, w, h, tc.ww, tc.wh)
		}
	}
}
