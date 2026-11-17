// Package api 是服务端接口的数据类型和客户端，桌面客户端只依赖这个包，不依赖 Telegram 的库。
package api

// Ping 是 /api/ping 的响应，不需要令牌，用来确认地址是 tg-stickers 的服务端
type Ping struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// 登录进行到哪一步
const (
	StepCredentials = "credentials" // 还没有 api_id / api_hash
	StepPhone       = "phone"       // 等手机号
	StepCode        = "code"        // 已经向 Telegram 要了验证码，等验证码；有没有真的发出看 State.CodeSent
	StepPassword    = "password"    // 账号开了两步验证，等密码
	StepQR          = "qr"          // 扫码登录，等手机上的 Telegram 扫 State.QR
	StepReady       = "ready"       // 已登录
	StepConnecting  = "connecting"  // 正在连接 Telegram，稍后再查
)

// State 是 /api/state 的响应
type State struct {
	Version string `json:"version"`
	Step    string `json:"step"`
	// CodeVia 说明验证码发到了哪里（Telegram App、短信……），Step 为 code 时有
	CodeVia string `json:"code_via,omitempty"`
	// CodeSent 为假时 Telegram 没有用这个工具能收到的方式发出验证码，看 CodeNotice
	CodeSent bool `json:"code_sent"`
	// CodeNotice 是验证码去哪里找、或者为什么收不到的说明
	CodeNotice string `json:"code_notice,omitempty"`
	// CodeNext 是重新发送时 Telegram 会改用的方式；为空表示没有别的方式
	CodeNext string `json:"code_next,omitempty"`
	// CodeCanResend 为真时 Telegram 提供了下一种发送方式，到了 CodeRetryAt 才能重发
	CodeCanResend bool `json:"code_can_resend"`
	// CodeRetryAt 是能重新发送的时间（Unix 秒）；0 表示不用等
	CodeRetryAt int64 `json:"code_retry_at,omitempty"`
	// QR 是扫码登录的二维码，Step 为 qr 时有；刚开始还在生成时为空
	QR *LoginQR `json:"qr,omitempty"`
	// PasswordHint 是两步验证密码的提示，Step 为 password 时可能有
	PasswordHint string `json:"password_hint,omitempty"`
	// CredentialsFromEnv 为真时 api_id / api_hash 来自环境变量，客户端里不能改
	CredentialsFromEnv bool   `json:"credentials_from_env"`
	User               *User  `json:"user,omitempty"`
	Error              string `json:"error,omitempty"` // 连接 Telegram 或扫码登录出的错
	FFmpeg             string `json:"ffmpeg"`          // ffmpeg 版本；为空表示找不到 ffmpeg，没法转换
	Limits             Limits `json:"limits"`
}

// LoginQR 是扫码登录的二维码内容，过期前会换新的
type LoginQR struct {
	URL     string `json:"url"`     // tg://login?token=…，生成二维码用
	Expires int64  `json:"expires"` // 过期时间，Unix 秒
}

// Limits 是上传的限制，客户端用来提前提示
type Limits struct {
	MaxUploadBytes int64 `json:"max_upload_bytes"`
}

type User struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Phone    string `json:"phone,omitempty"`
}

type Credentials struct {
	APIID   int    `json:"api_id"`
	APIHash string `json:"api_hash"`
}

type PhoneRequest struct {
	Phone string `json:"phone"`
}

type CodeRequest struct {
	Code string `json:"code"`
}

type PasswordRequest struct {
	Password string `json:"password"`
}

// 贴纸的格式
const (
	KindStatic   = "static"   // PNG / WEBP
	KindVideo    = "video"    // WEBM
	KindAnimated = "animated" // TGS（Lottie），只能看和管理，不能从这里做
)

// SetSummary 是贴纸包列表里的一项
type SetSummary struct {
	ID        int64  `json:"id"`
	ShortName string `json:"short_name"`
	Title     string `json:"title"`
	Count     int    `json:"count"`
	// Cover 是封面贴纸的 ID，用 /api/stickers/{id}/thumb 取缩略图；0 表示没有
	Cover    int64 `json:"cover"`
	Emojis   bool  `json:"emojis"` // 自定义表情包
	Masks    bool  `json:"masks"`
	Archived bool  `json:"archived"`
}

// Set 是一个贴纸包的全部内容
type Set struct {
	SetSummary
	Stickers []Sticker `json:"stickers"`
}

type Sticker struct {
	ID    int64  `json:"id"`
	Emoji string `json:"emoji"`
	Kind  string `json:"kind"`
	Size  int64  `json:"size"`
}

// CreateSet 是新建贴纸包的表单字段，和第一张贴纸的文件一起用 multipart 上传
type CreateSet struct {
	Title     string
	ShortName string
	Upload
}

// Upload 是上传一张贴纸或一个 GIF 的选项
type Upload struct {
	// Emoji 是这张贴纸对应的表情，可以有多个，例如 "😀😂"
	Emoji string
	// Fit 是动图超过 3 秒时怎么办，见 Fit* 常量
	Fit string
	MediaOptions
}

// 动图超过视频贴纸 3 秒上限时的处理方式
const (
	FitSpeed = "speed" // 加速到 3 秒，整段动图都留着
	FitTrim  = "trim"  // 只取前 3 秒
)

// MediaOptions 在服务器上剪辑和缩放原文件。零值保留完整时段并使用自动尺寸、帧率和标准画质。
type MediaOptions struct {
	Start   float64 // 开始秒数，0 表示从头
	End     float64 // 结束秒数，0 表示直到原文件结尾
	Width   int     // 目标画布尺寸；宽高同时为 0 表示自动
	Height  int
	Resize  string // contain 等比缩放、pad 补边、crop 居中裁剪
	FPS     int    // 帧率上限，0 表示自动
	Quality string // standard、high、small
}

// SetPatch 修改贴纸包；只改不为空的字段
type SetPatch struct {
	Title string `json:"title,omitempty"`
}

// StickerPatch 修改一张贴纸；Position 为 nil 表示不动位置，从 0 开始
type StickerPatch struct {
	Emoji    string `json:"emoji,omitempty"`
	Position *int   `json:"position,omitempty"`
}

// ShortNameCheck 是 /api/short-names/{name} 的响应
type ShortNameCheck struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// GIF 是「GIF 收藏」里的一个
type GIF struct {
	ID   int64 `json:"id"`
	Size int64 `json:"size"`
	W    int   `json:"w"`
	H    int   `json:"h"`
}

// Error 是接口出错时的响应
type Error struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
}

func (e *Error) Error() string { return e.Message }
