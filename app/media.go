package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/VOfficial404/tg-stickers/api"
)

// mediaForm 保留输入文本，提交时才转换为传给服务端的值。
type mediaForm struct {
	start, end    string
	width, height string
	fps           string
	resize        int
	quality       int
}

var resizeModes = []string{"contain", "pad", "crop"}
var qualityModes = []string{"standard", "high", "small"}

func mediaSeconds(text, label string) (float64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, fmt.Errorf("%s请输入非负秒数，可以含小数；空或 0 使用默认值", label)
	}
	return value, nil
}

func mediaInteger(text, label string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s请输入非负整数；空或 0 表示自动", label)
	}
	return value, nil
}

func (f *mediaForm) options(sticker bool) (api.MediaOptions, error) {
	var opts api.MediaOptions
	var err error
	if opts.Start, err = mediaSeconds(f.start, "开始时间"); err != nil {
		return opts, err
	}
	if opts.End, err = mediaSeconds(f.end, "结束时间"); err != nil {
		return opts, err
	}
	if opts.End != 0 && opts.End <= opts.Start {
		return opts, fmt.Errorf("结束时间必须大于开始时间，或留空 / 填 0 表示到原文件结尾")
	}
	if opts.Width, err = mediaInteger(f.width, "目标宽度"); err != nil {
		return opts, err
	}
	if opts.Height, err = mediaInteger(f.height, "目标高度"); err != nil {
		return opts, err
	}
	if (opts.Width == 0) != (opts.Height == 0) {
		return opts, fmt.Errorf("目标宽高必须同时填写，或都留空 / 填 0 使用自动尺寸")
	}
	if opts.Width != 0 {
		limit := 2048
		if sticker {
			limit = 512
		}
		if opts.Width < 2 || opts.Height < 2 || opts.Width > limit || opts.Height > limit || opts.Width%2 != 0 || opts.Height%2 != 0 {
			return opts, fmt.Errorf("目标宽高必须是 2 至 %d 的偶数", limit)
		}
		if sticker && max(opts.Width, opts.Height) != 512 {
			return opts, fmt.Errorf("贴纸目标尺寸的长边必须是 512，例如 512 × 512 或 512 × 288")
		}
	}
	if opts.FPS, err = mediaInteger(f.fps, "帧率"); err != nil {
		return opts, err
	}
	fpsLimit := 60
	if sticker {
		fpsLimit = 30
	}
	if opts.FPS > fpsLimit {
		return opts, fmt.Errorf("帧率必须是 1 至 %d 的整数，或留空 / 填 0 使用自动帧率", fpsLimit)
	}
	opts.Resize, opts.Quality = resizeModes[f.resize], qualityModes[f.quality]
	return opts, nil
}

func (a *app) uploadSnapshot(sticker bool) (api.Upload, error) {
	form := &a.gifMedia
	if sticker {
		form = &a.media
	}
	opts, err := form.options(sticker)
	if err != nil {
		return api.Upload{}, err
	}
	upload := api.Upload{MediaOptions: opts}
	if sticker {
		upload.Emoji, upload.Fit = a.emoji, fits[a.fit]
	}
	return upload, nil
}

func (a *app) mediaFields(c *ui.Context, f *mediaForm, enabled, sticker bool) {
	ui.Text(c, "云端转换选项").Bold().FontSize(13)
	ui.Row(c).Gap(12).Wrap().Children(func() {
		ui.Field(c, "开始（秒）", func() {
			ui.TextInput(c.Key("media-start"), &f.start).Placeholder("0").Width(110).Disabled(!enabled)
		})
		ui.Field(c, "结束（秒）", func() {
			ui.TextInput(c.Key("media-end"), &f.end).Placeholder("0 = 到结尾").Width(110).Disabled(!enabled)
		})
		ui.Field(c, "目标宽度", func() {
			ui.TextInput(c.Key("media-width"), &f.width).Placeholder("0 = 自动").Width(110).Disabled(!enabled)
		})
		ui.Field(c, "目标高度", func() {
			ui.TextInput(c.Key("media-height"), &f.height).Placeholder("0 = 自动").Width(110).Disabled(!enabled)
		})
		ui.Field(c, "帧率（FPS）", func() {
			ui.TextInput(c.Key("media-fps"), &f.fps).Placeholder("0 = 自动").Width(110).Disabled(!enabled)
		})
	})
	ui.Row(c).Gap(16).Wrap().Children(func() {
		ui.Field(c, "尺寸处理", func() {
			ui.Segmented(c.Key("media-resize"), &f.resize, "等比 contain", "补边 pad", "裁剪 crop").Disabled(!enabled)
		})
		ui.Field(c, "画质", func() {
			ui.Segmented(c.Key("media-quality"), &f.quality, "标准", "高清", "较小文件").Disabled(!enabled)
		})
	})
	muted(c, "时间留空或 0：从头 / 到原文件结尾；不能超出原文件时长。宽高同时留空或 0 为自动；contain 等比缩放到框内，pad 补边到目标尺寸，crop 居中裁剪。每个文件最多 50 MiB。").FontSize(12)
	if sticker {
		muted(c, "先按开始 / 结束时间裁选，再加速或取前 3 秒；视频贴纸始终最长 3 秒、长边 512、最多 30 FPS。自定宽高须为 2–512 的偶数且长边为 512；contain 若使输出长边不足 512，请改用透明补边 pad 或裁剪 crop。画质会按 Telegram 大小限制自动调整。").FontSize(12)
	} else {
		muted(c, "GIF 收藏超过 3 秒也保留所选时段的完整时长，不加速、不截为 3 秒。自定宽高为 2–2048 的偶数，FPS 为 1–60；自动尺寸最长 1280，不放大小图。pad 使用黑色补边，转换为无声 MP4。").FontSize(12)
	}
}
