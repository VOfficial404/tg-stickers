package convert

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/VOfficial404/tg-stickers/api"
)

type mediaPlan struct {
	filters  []string
	duration float64
	w, h     int
}

// planMedia 先选取原文件时段，再决定画布；三秒贴纸适配在此之后进行。
func planMedia(info Info, m api.MediaOptions, sticker bool) (mediaPlan, error) {
	if err := m.ValidateMedia(sticker); err != nil {
		return mediaPlan{}, err
	}
	p := mediaPlan{duration: info.Duration}
	if !info.Animated() {
		if m.Start != 0 || m.End != 0 {
			return p, errors.New("静态图片没有可截取的时间段，请将开始和结束时间留空")
		}
	} else {
		if info.Duration <= 0 {
			return p, errors.New("读取不到动图时长，无法确定要转换的时间段")
		}
		end := info.Duration
		if m.End != 0 {
			if m.End > info.Duration+0.001 {
				return p, fmt.Errorf("结束时间超过原文件时长 %.3f 秒", info.Duration)
			}
			end = min(end, m.End)
		}
		if m.Start >= end {
			return p, fmt.Errorf("开始时间必须早于原文件结尾（%.3f 秒）", info.Duration)
		}
		p.duration = end - m.Start
		if m.Start != 0 || m.End != 0 {
			p.filters = append(p.filters, "trim=start="+number(m.Start)+":end="+number(end))
		}
		p.filters = append(p.filters, "setpts=PTS-STARTPTS")
	}
	w, h := m.Width, m.Height
	if w == 0 {
		if sticker {
			w, h = StickerSide, StickerSide
		} else {
			w, h = evenFit(info.W, info.H, gifMaxSide)
		}
	}
	mode := m.Resize
	if mode == "" {
		mode = "contain"
	}
	// 限制边直接使用目标整数，避免 512/原宽 的浮点误差把合法贴纸误算成 510。
	var sw, sh int
	if int64(info.W)*int64(h) >= int64(info.H)*int64(w) {
		sw, sh = w, max(2, int(int64(info.H)*int64(w)/int64(info.W))&^1)
	} else {
		sw, sh = max(2, int(int64(info.W)*int64(h)/int64(info.H))&^1), h
	}
	if mode == "crop" {
		cw, ch := info.W, info.H
		if int64(cw)*int64(h) > int64(ch)*int64(w) {
			cw = max(1, ch*w/h)
		} else {
			ch = max(1, cw*h/w)
		}
		p.filters = append(p.filters, fmt.Sprintf("crop=%d:%d:(iw-ow)/2:(ih-oh)/2:exact=1", cw, ch))
		sw, sh = w, h
	}
	p.filters = append(p.filters, fmt.Sprintf("scale=%d:%d:flags=lanczos", sw, sh), "setsar=1")
	p.w, p.h = sw, sh
	switch mode {
	case "pad":
		color := "black"
		if sticker {
			p.filters = append(p.filters, "format=rgba")
			color = "black@0"
		}
		p.filters = append(p.filters, fmt.Sprintf("pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=%s", w, h, color))
		p.w, p.h = w, h
	}
	if sticker && max(p.w, p.h) != StickerSide {
		return p, errors.New("等比缩放后的贴纸长边没有达到 512；请改用补边或居中裁剪，或选择 512 × 512 画布")
	}
	return p, nil
}

func number(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

var (
	gifStandardCRFs = [...]int{23, 28, 33}
	gifHighCRFs     = [...]int{18, 23, 28, 33}
	gifSmallCRFs    = [...]int{30, 33, 36}
)

func gifCRFs(quality string) []int {
	switch quality {
	case "high":
		return gifHighCRFs[:]
	case "small":
		return gifSmallCRFs[:]
	default:
		return gifStandardCRFs[:]
	}
}

func stickerCRF(quality string) int {
	switch quality {
	case "high":
		return 24
	case "small":
		return 42
	default:
		return 30
	}
}
