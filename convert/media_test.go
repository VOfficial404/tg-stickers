package convert

import (
	"bytes"
	"context"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/VOfficial404/tg-stickers/api"
)

func TestLongGIFPreservesFullDuration(t *testing.T) {
	ff := tool(t)
	dir := t.TempDir()
	in := makeGIF(t, ff, dir, 160, 90, 6)
	g, err := ff.GIF(context.Background(), in, dir, api.MediaOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(g.Duration-6) > 0.1 {
		t.Fatalf("long GIF duration = %.3f, want full 6 seconds", g.Duration)
	}
}

func TestCloudClipCropFPSAndActualMetadata(t *testing.T) {
	ff := tool(t)
	ctx := context.Background()
	dir := t.TempDir()
	in := filepath.Join(dir, "red-then-blue.gif")
	if err := ff.run(ctx, "-f", "lavfi", "-i", "color=c=red:s=120x80:r=12:d=2[r];color=c=blue:s=120x80:r=12:d=4[b];[r][b]concat=n=2:v=1:a=0", in); err != nil {
		t.Fatal(err)
	}
	g, err := ff.GIF(ctx, in, dir, api.MediaOptions{Start: 2.5, End: 6, Width: 80, Height: 80, Resize: "crop", FPS: 10, Quality: "small"})
	if err != nil {
		t.Fatal(err)
	}
	if g.W != 80 || g.H != 80 || math.Abs(g.Duration-3.5) > 0.11 {
		t.Fatalf("actual upload metadata = %dx%d %.3fs, want 80x80 3.5s", g.W, g.H, g.Duration)
	}
	info, err := ff.Probe(ctx, g.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.FPS > 10.01 {
		t.Fatalf("FPS = %.3f, want at most 10", info.FPS)
	}
	frame, err := ff.Frame(ctx, g.Path, dir)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
	if b < 50000 || r > 10000 {
		t.Fatalf("clip kept red opening instead of selected blue segment: R=%d B=%d", r, b)
	}
}

func TestStaticStickerPaddingIsTransparent(t *testing.T) {
	ff := tool(t)
	ctx := context.Background()
	dir := t.TempDir()
	in := filepath.Join(dir, "red.png")
	if err := ff.run(ctx, "-f", "lavfi", "-i", "color=c=red:s=120x80", "-frames:v", "1", in); err != nil {
		t.Fatal(err)
	}
	st, err := ff.Sticker(ctx, in, dir, api.Upload{MediaOptions: api.MediaOptions{Width: 512, Height: 512, Resize: "pad"}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, border := img.At(0, 0).RGBA()
	_, _, _, center := img.At(256, 256).RGBA()
	if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 512 || border != 0 || center == 0 {
		t.Fatalf("invalid transparent padding: bounds=%v border=%d center=%d", img.Bounds(), border, center)
	}
}

func TestSelectedVideoStickerStillMeetsTelegramLimit(t *testing.T) {
	ff := tool(t)
	ctx := context.Background()
	dir := t.TempDir()
	in := makeGIF(t, ff, dir, 200, 120, 7)
	st, err := ff.Sticker(ctx, in, dir, api.Upload{Fit: "speed", MediaOptions: api.MediaOptions{Start: 2, End: 6, Width: 512, Height: 512, Resize: "crop", FPS: 12, Quality: "small"}})
	if err != nil {
		t.Fatal(err)
	}
	info, err := ff.Probe(ctx, st.Path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.W != 512 || info.H != 512 || info.FPS > 12.01 || info.Duration > 3 || fi.Size() > MaxVideoBytes {
		t.Fatalf("custom sticker broke Telegram limits: %+v bytes=%d", info, fi.Size())
	}
}

func TestDefaultStickerKeepsExactLongEdge(t *testing.T) {
	for _, long := range []int{49, 98, 103, 161, 196, 206, 322} {
		for _, size := range [][2]int{{long, long}, {long, long / 2}, {long / 2, long}} {
			plan, err := planMedia(Info{W: size[0], H: size[1]}, api.MediaOptions{}, true)
			if err != nil || max(plan.w, plan.h) != 512 {
				t.Errorf("default sticker %dx%d rejected or shrunk: %dx%d, %v", size[0], size[1], plan.w, plan.h, err)
			}
		}
	}
}

// 左红右蓝的图，框选右下角一块：结果必须是蓝色，尺寸按选区计算
func TestSourceSelectionCropsChosenRegion(t *testing.T) {
	ff := tool(t)
	ctx := context.Background()
	dir := t.TempDir()
	in := filepath.Join(dir, "halves.png")
	if err := ff.run(ctx, "-f", "lavfi", "-i", "color=c=red:s=200x100[l];color=c=blue:s=200x100[r];[l][r]hstack", "-frames:v", "1", in); err != nil {
		t.Fatal(err)
	}
	sel := api.MediaOptions{CropX: 260, CropY: 20, CropW: 120, CropH: 60, Width: 512, Height: 256, Resize: "pad"}
	st, err := ff.Sticker(ctx, in, dir, api.Upload{MediaOptions: sel})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ := img.At(256, 128).RGBA()
	if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 256 || b < 50000 || r > 10000 {
		t.Fatalf("selection not applied: bounds=%v R=%d B=%d", img.Bounds(), r, b)
	}
	if _, err := planMedia(Info{W: 400, H: 100}, api.MediaOptions{CropX: 300, CropW: 120, CropH: 60}, false); err == nil {
		t.Fatal("selection outside the frame was accepted")
	}
	plan, err := planMedia(Info{W: 4000, H: 3000}, api.MediaOptions{CropX: 100, CropY: 100, CropW: 600, CropH: 300}, false)
	if err != nil || plan.w != 600 || plan.h != 300 {
		t.Fatalf("auto GIF size must follow the selection: %dx%d, %v", plan.w, plan.h, err)
	}
}
