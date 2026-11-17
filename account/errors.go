package account

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tgerr"
)

// Error 带着该回给客户端的 HTTP 状态码
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func userError(msg string) error   { return &Error{http.StatusBadRequest, msg} }
func notFound(msg string) error    { return &Error{http.StatusNotFound, msg} }
func unavailable(msg string) error { return &Error{http.StatusServiceUnavailable, msg} }

// unauthorized 是 Telegram 账号没登录；不用 401，401 留给令牌不对
func unauthorized(msg string) error { return &Error{http.StatusConflict, msg} }

// Telegram 的错误类型对应的说明
var tgMessages = map[string]string{
	"API_ID_INVALID":           "api_id 或 api_hash 不对，到 my.telegram.org 核对",
	"API_ID_PUBLISHED_FLOOD":   "这个 api_id 被 Telegram 限制了，换一个自己申请的",
	"PHONE_NUMBER_INVALID":     "手机号不对，要带国家码，例如 +8613800000000",
	"PHONE_NUMBER_BANNED":      "这个手机号被 Telegram 封禁了",
	"PHONE_NUMBER_FLOOD":       "这个手机号请求验证码太频繁，过一段时间再试",
	"PHONE_CODE_INVALID":       "验证码不对",
	"PHONE_CODE_EMPTY":         "请填写验证码",
	"PHONE_CODE_EXPIRED":       "验证码过期了，重新获取一个",
	"SEND_CODE_UNAVAILABLE":    "Telegram 当前没有可用的验证码发送渠道。请使用已登录的手机扫码，或稍后在官方手机 App 登录",
	"AUTH_RESTART":             "Telegram 要求重新开始登录，请返回手机号页或重新生成二维码",
	"PHONE_PASSWORD_FLOOD":     "两步验证尝试过于频繁，请等待后再试",
	"PHONE_NUMBER_UNOCCUPIED":  "这个号码尚未注册 Telegram，请先在官方手机 App 注册",
	"SMS_CODE_CREATE_FAILED":   "Telegram 未能创建短信验证码，可改用已登录的手机扫码",
	"PASSWORD_HASH_INVALID":    "两步验证密码不对",
	"SESSION_PASSWORD_NEEDED":  "账号开了两步验证，需要填密码",
	"STICKERSET_INVALID":       "找不到这个贴纸包，或者它不是你创建的",
	"STICKERS_TOO_MUCH":        "贴纸包满了，普通贴纸包最多 120 张",
	"STICKERS_EMPTY":           "新贴纸包至少要有一张贴纸",
	"STICKER_EMOJI_INVALID":    "表情不对，要填真正的 emoji，例如 😀",
	"STICKER_FILE_INVALID":     "Telegram 不接受这个贴纸文件",
	"STICKER_VIDEO_BIG":        "视频贴纸超过 256 KB",
	"STICKER_VIDEO_LONG":       "视频贴纸超过 3 秒",
	"STICKER_VIDEO_NOWEBM":     "视频贴纸不是 WEBM 格式",
	"STICKER_GIF_DIMENSIONS":   "视频贴纸的尺寸不对，长边要正好 512",
	"STICKER_PNG_DIMENSIONS":   "静态贴纸的尺寸不对，长边要正好 512",
	"STICKER_PNG_NOPNG":        "静态贴纸不是有效的 PNG",
	"STICKER_INVALID":          "这张贴纸不在你的贴纸包里",
	"STICKER_ID_INVALID":       "这张贴纸不在你的贴纸包里",
	"SHORT_NAME_INVALID":       "链接名只能用英文字母、数字和下划线，以字母开头，不能有连续的下划线",
	"SHORT_NAME_OCCUPIED":      "链接名已经被别人用了，换一个",
	"SHORTNAME_OCCUPY_FAILED":  "链接名已经被别人用了，换一个",
	"PACK_TITLE_INVALID":       "贴纸包名字不对，长度 1–64 个字",
	"PACK_SHORT_NAME_INVALID":  "链接名只能用英文字母、数字和下划线，以字母开头，不能有连续的下划线",
	"PACK_SHORT_NAME_OCCUPIED": "链接名已经被别人用了，换一个",
	"FILE_PARTS_INVALID":       "文件上传不完整，再试一次",
	"MEDIA_EMPTY":              "Telegram 不接受这个文件",
	"DOCUMENT_INVALID":         "这个文件已经不在 Telegram 上了，刷新一下",
	"FILE_REFERENCE_EXPIRED":   "文件引用过期了，刷新一下",
	"USER_DEACTIVATED_BAN":     "账号被 Telegram 封禁了",
	"FROZEN_METHOD_INVALID":    "账号被 Telegram 冻结了，暂时不能做这个操作",
}

// humanize 把 Telegram 的错误换成看得懂的说明
func humanize(err error) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	if d, ok := tgerr.AsFloodWait(err); ok {
		return &Error{http.StatusTooManyRequests, fmt.Sprintf("操作太频繁，Telegram 要求等 %s 再试", d.Round(time.Second))}
	}
	if rpc, ok := tgerr.As(err); ok {
		if msg, ok := tgMessages[rpc.Type]; ok {
			return &Error{http.StatusBadRequest, msg}
		}
		return &Error{http.StatusBadGateway, "Telegram 返回错误 " + rpc.Message}
	}
	return &Error{http.StatusBadGateway, err.Error()}
}

// isAuthLost 表示会话已经失效：在别的设备上被踢掉、账号被删
func isAuthLost(err error) bool {
	return auth.IsUnauthorized(err) || tgerr.Is(err, "SESSION_REVOKED", "SESSION_EXPIRED", "USER_DEACTIVATED")
}
