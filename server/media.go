package server

import (
	"strconv"
	"strings"

	"github.com/VOfficial404/tg-stickers/api"
)

// mediaOptions 只解析已知数值和枚举字段，转换器不接受用户提供的 ffmpeg 表达式。
func mediaOptions(form map[string]string, sticker bool) (api.Upload, error) {
	u := api.Upload{Emoji: form["emoji"], Fit: form["fit"]}
	for _, field := range [...]struct {
		key string
		dst *float64
	}{{"start", &u.Start}, {"end", &u.End}} {
		key, dst := field.key, field.dst
		s := strings.TrimSpace(form[key])
		if s == "" {
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return u, badRequest(key + " 必须填写秒数")
		}
		*dst = v
	}
	for _, field := range [...]struct {
		key string
		dst *int
	}{{"width", &u.Width}, {"height", &u.Height}, {"fps", &u.FPS},
		{"crop_x", &u.CropX}, {"crop_y", &u.CropY}, {"crop_w", &u.CropW}, {"crop_h", &u.CropH}} {
		key, dst := field.key, field.dst
		s := strings.TrimSpace(form[key])
		if s == "" {
			continue
		}
		v, err := strconv.Atoi(s)
		if err != nil {
			return u, badRequest(key + " 必须填写整数")
		}
		*dst = v
	}
	u.Resize, u.Quality = form["resize"], form["quality"]
	if err := u.MediaOptions.ValidateMedia(sticker); err != nil {
		return u, badRequest(err.Error())
	}
	if sticker && u.Fit != "" && u.Fit != api.FitSpeed && u.Fit != api.FitTrim {
		return u, badRequest("超过 3 秒的贴纸只能选择加速或截取")
	}
	return u, nil
}
