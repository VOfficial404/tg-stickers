// Package convert 用 ffmpeg 把图片和动图转换成 Telegram 要的格式：
// 视频贴纸是 WEBM（VP9），静态贴纸是 PNG 或 WEBP，GIF 收藏是无声 MP4。
package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/VOfficial404/tg-stickers/api"
)

// Telegram 对贴纸的要求，见 https://core.telegram.org/stickers
const (
	StickerSide      = 512
	MaxVideoSeconds  = 3.0
	MaxVideoFPS      = 30
	MaxVideoBytes    = 256 << 10
	MaxStaticBytes   = 512 << 10
	videoTargetSecs  = 2.99 // 留一点余量，避免编码后的时长四舍五入超过 3 秒
	ThumbSide        = 320
	gifMaxSide       = 1280
	gifSmallFileSize = 10 << 20
)

// Tool 是一对 ffmpeg / ffprobe 的路径
type Tool struct {
	FFmpeg, FFprobe string
}

// Find 找 ffmpeg：ffmpegPath 不为空时用它，否则在 PATH 里找；ffprobe 优先找和 ffmpeg 放在一起的
func Find(ffmpegPath string) (Tool, error) {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	ff, err := exec.LookPath(ffmpegPath)
	if err != nil {
		return Tool{}, fmt.Errorf("找不到 ffmpeg（%s），装上 ffmpeg 或用 TG_STICKERS_FFMPEG 指定路径", ffmpegPath)
	}
	probe := filepath.Join(filepath.Dir(ff), "ffprobe"+filepath.Ext(ff))
	if _, err := os.Stat(probe); err != nil {
		if probe, err = exec.LookPath("ffprobe"); err != nil {
			return Tool{}, errors.New("找到了 ffmpeg 但找不到 ffprobe，它们一般在同一个安装包里")
		}
	}
	return Tool{FFmpeg: ff, FFprobe: probe}, nil
}

// Version 返回 ffmpeg 版本号那一行里的版本，例如 "7.1"
func (t Tool) Version(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, t.FFmpeg, "-hide_banner", "-version").Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(out), "\n")
	if f := strings.Fields(line); len(f) >= 3 {
		return f[2]
	}
	return strings.TrimSpace(line)
}

func (t Tool) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, t.FFmpeg, append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("ffmpeg 转换失败：%s", msg)
	}
	return nil
}

// Info 是 ffprobe 看到的第一路视频流
type Info struct {
	W, H     int
	Frames   int     // 帧数
	Duration float64 // 秒；静态图片是 0
	FPS      float64 // 帧率；拿不到时是 0
	Codec    string
}

// Animated 表示有不止一帧
func (i Info) Animated() bool { return i.Frames > 1 }

func (t Tool) Probe(ctx context.Context, path string) (Info, error) {
	out, err := exec.CommandContext(ctx, t.FFprobe, "-v", "error", "-select_streams", "v:0", "-count_packets",
		"-show_entries", "stream=codec_name,width,height,avg_frame_rate,r_frame_rate,nb_read_packets,duration:format=duration",
		"-of", "json", path).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return Info{}, fmt.Errorf("读不出这个文件：%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return Info{}, fmt.Errorf("读不出这个文件：%v", err)
	}
	var r struct {
		Streams []struct {
			Codec    string `json:"codec_name"`
			W        int    `json:"width"`
			H        int    `json:"height"`
			Avg      string `json:"avg_frame_rate"`
			Real     string `json:"r_frame_rate"`
			Packets  string `json:"nb_read_packets"`
			Duration string `json:"duration"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return Info{}, fmt.Errorf("ffprobe 的输出看不懂：%v", err)
	}
	if len(r.Streams) == 0 || r.Streams[0].W == 0 || r.Streams[0].H == 0 {
		return Info{}, errors.New("这个文件里没有图片或视频")
	}
	s := r.Streams[0]
	info := Info{W: s.W, H: s.H, Codec: s.Codec}
	info.Frames, _ = strconv.Atoi(s.Packets)
	info.Duration = parseFloat(s.Duration)
	if info.Duration <= 0 {
		info.Duration = parseFloat(r.Format.Duration)
	}
	info.FPS = parseRate(s.Avg)
	if info.FPS <= 0 {
		info.FPS = parseRate(s.Real)
	}
	if info.Animated() && info.Duration <= 0 && info.FPS > 0 {
		info.Duration = float64(info.Frames) / info.FPS
	}
	if !info.Animated() {
		info.Duration = 0
	} else if info.FPS <= 0 && info.Duration > 0 {
		info.FPS = float64(info.Frames) / info.Duration
	}
	return info, nil
}

func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// parseRate 解析 ffprobe 的 "30000/1001" 这种帧率
func parseRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		return parseFloat(s)
	}
	n, d := parseFloat(num), parseFloat(den)
	if d == 0 {
		return 0
	}
	return n / d
}

// Sticker 是转换好的贴纸文件
type Sticker struct {
	Path string
	Mime string // video/webm、image/png 或 image/webp
	Name string // 上传时用的文件名
}

// 视频贴纸逐步降低质量，直到不超过 256KB
var videoAttempts = [...]struct {
	crf    int
	maxFPS float64
}{
	{24, MaxVideoFPS}, {30, MaxVideoFPS}, {36, MaxVideoFPS}, {42, MaxVideoFPS}, {48, MaxVideoFPS},
	{52, 24}, {56, 20}, {60, 15}, {63, 12},
}

// Sticker 在服务器上剪辑、调整尺寸并转换贴纸，最后仍需满足 Telegram 的三秒和 256 KB 限制。
func (t Tool) Sticker(ctx context.Context, in, dir string, opts api.Upload) (Sticker, error) {
	info, err := t.Probe(ctx, in)
	if err != nil {
		return Sticker{}, err
	}
	plan, err := planMedia(info, opts.MediaOptions, true)
	if err != nil {
		return Sticker{}, err
	}
	if !info.Animated() {
		return t.staticSticker(ctx, in, dir, info, plan, opts.Quality)
	}
	return t.videoSticker(ctx, in, dir, info, plan, opts)
}

// input 是读 in 的参数。ffmpeg 自带的 VP8/VP9 解码器会丢掉透明通道，带透明的 WEBM 要用 libvpx 解码。
func input(in string, info Info) []string {
	switch info.Codec {
	case "vp9":
		return []string{"-c:v", "libvpx-vp9", "-i", in}
	case "vp8":
		return []string{"-c:v", "libvpx", "-i", in}
	}
	return []string{"-i", in}
}

func (t Tool) videoSticker(ctx context.Context, in, dir string, info Info, plan mediaPlan, opts api.Upload) (Sticker, error) {
	out := filepath.Join(dir, "sticker.webm")
	srcFPS, outputDuration := info.FPS, plan.duration
	baseVF := strings.Join(plan.filters, ",")
	if plan.duration > MaxVideoSeconds {
		outputDuration = videoTargetSecs
		if opts.Fit != api.FitTrim {
			factor := videoTargetSecs / plan.duration
			baseVF += ",setpts=PTS*" + number(factor)
			srcFPS /= factor
		}
	}
	maxFPS := float64(MaxVideoFPS)
	if opts.FPS > 0 {
		maxFPS = float64(opts.FPS)
	}
	firstCRF := stickerCRF(opts.Quality)
	// 高画质多试一次；后续编码始终只会降低帧率/画质以满足 Telegram 文件限制。
	var lastSize int64
	for _, attempt := range videoAttempts {
		if attempt.crf < firstCRF {
			continue
		}
		fps := min(maxFPS, attempt.maxFPS)
		vf := baseVF
		if srcFPS <= 0 || srcFPS > fps {
			vf += ",fps=" + number(fps)
		}
		vf += ",format=yuva420p"
		cmd := append(input(in, info), "-t", number(outputDuration), "-an", "-sn", "-map_metadata", "-1", "-vf", vf,
			"-c:v", "libvpx-vp9", "-pix_fmt", "yuva420p", "-b:v", "0", "-crf", strconv.Itoa(attempt.crf),
			"-deadline", "good", "-cpu-used", "2", "-row-mt", "1", "-auto-alt-ref", "0", "-f", "webm", out)
		if err := t.run(ctx, cmd...); err != nil {
			return Sticker{}, err
		}
		st, err := os.Stat(out)
		if err != nil {
			return Sticker{}, err
		}
		if lastSize = st.Size(); lastSize <= MaxVideoBytes {
			return Sticker{Path: out, Mime: "video/webm", Name: "sticker.webm"}, nil
		}
	}
	return Sticker{}, fmt.Errorf("压缩后还有 %d KB，超过视频贴纸 256 KB 上限，请缩短所选时段或减少画面细节", lastSize>>10)
}

func (t Tool) staticSticker(ctx context.Context, in, dir string, info Info, plan mediaPlan, quality string) (Sticker, error) {
	png := filepath.Join(dir, "sticker.png")
	vf := strings.Join(plan.filters, ",") + ",format=rgba"
	if err := t.run(ctx, append(input(in, info), "-frames:v", "1", "-map_metadata", "-1", "-vf", vf,
		"-compression_level", "100", png)...); err != nil {
		return Sticker{}, err
	}
	fi, err := os.Stat(png)
	if err != nil {
		return Sticker{}, err
	}
	if fi.Size() <= MaxStaticBytes && quality != "small" {
		return Sticker{Path: png, Mime: "image/png", Name: "sticker.png"}, nil
	}
	webp := filepath.Join(dir, "sticker.webp")
	q := 90
	if quality == "high" {
		q = 95
	}
	if quality == "small" {
		q = 65
	}
	for _, value := range []int{q, 60, 40} {
		if err := t.run(ctx, "-i", png, "-c:v", "libwebp", "-quality", strconv.Itoa(value), webp); err != nil {
			return Sticker{}, err
		}
		if st, err := os.Stat(webp); err == nil && st.Size() <= MaxStaticBytes {
			return Sticker{Path: webp, Mime: "image/webp", Name: "sticker.webp"}, nil
		}
	}
	return Sticker{}, errors.New("图片压缩后仍超过静态贴纸 512 KB 的上限")
}

// GIF 是转换好的 GIF 收藏文件
type GIF struct {
	Path     string
	W, H     int
	Duration float64
}

// GIF 把所选时段转换为无声 H.264 MP4；不受视频贴纸三秒限制，自动尺寸长边不超过 1280。
func (t Tool) GIF(ctx context.Context, in, dir string, opts api.MediaOptions) (GIF, error) {
	info, err := t.Probe(ctx, in)
	if err != nil {
		return GIF{}, err
	}
	if !info.Animated() {
		return GIF{}, errors.New("这是一张静态图片，GIF 收藏只收动图")
	}
	plan, err := planMedia(info, opts, false)
	if err != nil {
		return GIF{}, err
	}
	out := filepath.Join(dir, "animation.mp4")
	vf := strings.Join(plan.filters, ",")
	fps := 60
	if opts.FPS > 0 {
		fps = opts.FPS
	}
	if info.FPS <= 0 || info.FPS > float64(fps) {
		vf += ",fps=" + strconv.Itoa(fps)
	}
	vf += ",format=yuv420p"
	for _, crf := range gifCRFs(opts.Quality) {
		if err := t.run(ctx, append(input(in, info), "-t", number(plan.duration), "-an", "-sn", "-map_metadata", "-1", "-vf", vf,
			"-c:v", "libx264", "-preset", "medium", "-crf", strconv.Itoa(crf), "-pix_fmt", "yuv420p",
			"-movflags", "+faststart", "-f", "mp4", out)...); err != nil {
			return GIF{}, err
		}
		if st, err := os.Stat(out); err == nil && st.Size() <= gifSmallFileSize {
			break
		}
	}
	// 上传的宽高和时长必须与实际编码结果一致（帧率取整可能使时长略有变化）。
	actual, err := t.Probe(ctx, out)
	if err != nil {
		return GIF{}, err
	}
	return GIF{Path: out, W: actual.W, H: actual.H, Duration: actual.Duration}, nil
}

// evenFit 把 w×h 等比缩到长边不超过 side，并取偶数
func evenFit(w, h, side int) (int, int) {
	if w > side || h > side {
		if w >= h {
			h, w = h*side/w, side
		} else {
			w, h = w*side/h, side
		}
	}
	return max(2, w&^1), max(2, h&^1)
}

// Frame 取 in 的第一帧，缩到长边不超过 320，存成 PNG 返回内容。用来给没有缩略图的视频做缩略图。
func (t Tool) Frame(ctx context.Context, in, dir string) ([]byte, error) {
	info, err := t.Probe(ctx, in)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "frame.png")
	vf := fmt.Sprintf("scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease", ThumbSide, ThumbSide)
	if err := t.run(ctx, append(input(in, info), "-frames:v", "1", "-vf", vf, out)...); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

func fmtFloat(f float64) string { return strconv.FormatFloat(f, 'f', 4, 64) }
