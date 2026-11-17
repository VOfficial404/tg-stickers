package api

import (
	"errors"
	"math"
)

// ValidateMedia 检查用户可控的转换参数；不接受原样 ffmpeg 参数或过滤器表达式。
func (m MediaOptions) ValidateMedia(sticker bool) error {
	if math.IsNaN(m.Start) || math.IsInf(m.Start, 0) || math.IsNaN(m.End) || math.IsInf(m.End, 0) || m.Start < 0 || m.End < 0 {
		return errors.New("开始和结束时间必须是大于等于 0 的秒数")
	}
	if m.End != 0 && m.End <= m.Start {
		return errors.New("结束时间必须晚于开始时间；留空或填 0 表示直到结尾")
	}
	if (m.Width == 0) != (m.Height == 0) {
		return errors.New("请同时填写目标宽度和高度，或同时留空使用自动尺寸")
	}
	if m.Width != 0 || m.Height != 0 {
		if m.Width < 2 || m.Height < 2 || m.Width%2 != 0 || m.Height%2 != 0 {
			return errors.New("目标宽度和高度必须是至少为 2 的偶数")
		}
		if sticker {
			if max(m.Width, m.Height) != 512 {
				return errors.New("贴纸的目标尺寸长边必须为 512，短边不能超过 512")
			}
		} else if m.Width > 2048 || m.Height > 2048 {
			return errors.New("GIF 目标宽度和高度不能超过 2048 像素")
		}
	}
	switch m.Resize {
	case "", "contain", "pad", "crop":
	default:
		return errors.New("缩放方式只能选择等比缩放、补边或居中裁剪")
	}
	maxFPS := 60
	if sticker {
		maxFPS = 30
	}
	if m.FPS < 0 || m.FPS > maxFPS {
		if sticker {
			return errors.New("贴纸帧率应为 0（自动）或 1–30")
		}
		return errors.New("GIF 帧率应为 0（自动）或 1–60")
	}
	switch m.Quality {
	case "", "standard", "high", "small":
	default:
		return errors.New("画质只能选择标准、高清或较小文件")
	}
	return nil
}
