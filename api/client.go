package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Client 是服务端接口的客户端，桌面客户端用它
type Client struct {
	base  string
	token string
	hc    *http.Client
}

// 普通请求的超时；上传要转换、要传到 Telegram，单独放宽
const (
	requestTimeout = 30 * time.Second
	uploadTimeout  = 5 * time.Minute
)

// NormalizeBase 整理用户填的服务器地址：补上 http://，去掉末尾的斜杠
func NormalizeBase(s string) (string, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" {
		return "", errors.New("请填写服务器地址")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("服务器地址不对，应写成 http://127.0.0.1:8787 或 https://stickers.example.com")
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

// NewClient 返回连接 base（NormalizeBase 整理过的地址）的客户端
func NewClient(base, token string) *Client {
	return &Client{base: base, token: strings.TrimSpace(token), hc: &http.Client{}}
}

func (c *Client) Base() string { return c.base }

func (c *Client) send(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			if ue.Timeout() || errors.Is(err, context.DeadlineExceeded) {
				return errors.New("等服务器响应超时")
			}
			err = ue.Err
		}
		return fmt.Errorf("连不上服务器：%v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("读取服务器的响应失败：%v", err)
	}
	if resp.StatusCode != http.StatusOK {
		e := &Error{Status: resp.StatusCode}
		if json.Unmarshal(data, e) != nil || e.Message == "" {
			e.Message = fmt.Sprintf("服务器返回 HTTP %d，这个地址可能不是 tg-stickers", resp.StatusCode)
		}
		return e
	}
	switch out := out.(type) {
	case nil:
		return nil
	case *[]byte:
		*out = data
		return nil
	default:
		if err := json.Unmarshal(data, out); err != nil {
			return errors.New("服务器的响应看不懂，这个地址可能不是 tg-stickers，或者版本不一致")
		}
		return nil
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var rd io.Reader
	contentType := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd, contentType = bytes.NewReader(b), "application/json"
	}
	return c.send(ctx, method, path, contentType, rd, out)
}

// upload 把 fields 和文件 path 用 multipart 发给服务端，文件边读边发
func (c *Client) upload(ctx context.Context, path string, fields map[string]string, file string, out any) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		for k, v := range fields {
			if err := mw.WriteField(k, v); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		part, err := mw.CreateFormFile("file", filepath.Base(file))
		if err == nil {
			_, err = io.Copy(part, f)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	err = c.send(ctx, http.MethodPost, path, mw.FormDataContentType(), pr, out)
	pr.Close()
	return err
}

// IsUnauthorized 表示令牌不对
func IsUnauthorized(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusUnauthorized
}

// IsNotFound 表示要的东西在服务端不存在了（例如贴纸已经删掉）
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// Ping 确认地址是 tg-stickers 的服务端，不需要令牌
func (c *Client) Ping(ctx context.Context) (p Ping, err error) {
	err = c.do(ctx, http.MethodGet, "/api/ping", nil, &p)
	if err == nil && p.Name != "tg-stickers" {
		err = errors.New("这个地址不是 tg-stickers 的服务端")
	}
	return p, err
}

// 下面的方法都用具名返回值：`return v, c.do(…, &v)` 里读 v 和调用 do 的先后 Go 没有规定。

func (c *Client) State(ctx context.Context) (s State, err error) {
	err = c.do(ctx, http.MethodGet, "/api/state", nil, &s)
	return s, err
}

// SetCredentials 保存 my.telegram.org 上申请的 api_id 和 api_hash
func (c *Client) SetCredentials(ctx context.Context, cr Credentials) (s State, err error) {
	err = c.do(ctx, http.MethodPut, "/api/credentials", cr, &s)
	return s, err
}

// SendCode 让 Telegram 给手机号发验证码
func (c *Client) SendCode(ctx context.Context, phone string) (s State, err error) {
	err = c.do(ctx, http.MethodPost, "/api/login/phone", PhoneRequest{Phone: phone}, &s)
	return s, err
}

// ResendCode 按 Telegram 返回的等待时间和下一发送渠道重发，保留当前登录请求。
func (c *Client) ResendCode(ctx context.Context) (s State, err error) {
	err = c.do(ctx, http.MethodPost, "/api/login/resend", nil, &s)
	return s, err
}

// StartQR 开始手机 Telegram 扫码登录；二维码和后续状态由 State 返回。
func (c *Client) StartQR(ctx context.Context) (s State, err error) {
	err = c.do(ctx, http.MethodPost, "/api/login/qr", nil, &s)
	return s, err
}

// QRImage 获取当前登录二维码；调用方应在二维码 URL 改变时重新取图。
func (c *Client) QRImage(ctx context.Context) (b []byte, err error) {
	err = c.do(ctx, http.MethodGet, "/api/login/qr.png", nil, &b)
	return b, err
}

// SignIn 提交验证码；账号开了两步验证时返回的 Step 是 password
func (c *Client) SignIn(ctx context.Context, code string) (s State, err error) {
	err = c.do(ctx, http.MethodPost, "/api/login/code", CodeRequest{Code: code}, &s)
	return s, err
}

func (c *Client) Password(ctx context.Context, password string) (s State, err error) {
	err = c.do(ctx, http.MethodPost, "/api/login/password", PasswordRequest{Password: password}, &s)
	return s, err
}

// CancelLogin 放弃进行到一半的登录，回到填手机号
func (c *Client) CancelLogin(ctx context.Context) (s State, err error) {
	err = c.do(ctx, http.MethodDelete, "/api/login", nil, &s)
	return s, err
}

// Logout 在 Telegram 上注销这次登录，并删掉服务端保存的会话
func (c *Client) Logout(ctx context.Context) (s State, err error) {
	err = c.do(ctx, http.MethodPost, "/api/logout", nil, &s)
	return s, err
}

// Sets 列出账号创建的贴纸包
func (c *Client) Sets(ctx context.Context) (s []SetSummary, err error) {
	err = c.do(ctx, http.MethodGet, "/api/sets", nil, &s)
	return s, err
}

func (c *Client) Set(ctx context.Context, shortName string) (s Set, err error) {
	err = c.do(ctx, http.MethodGet, "/api/sets/"+url.PathEscape(shortName), nil, &s)
	return s, err
}

// CheckShortName 查询链接名能不能用
func (c *Client) CheckShortName(ctx context.Context, name string) (r ShortNameCheck, err error) {
	err = c.do(ctx, http.MethodGet, "/api/short-names/"+url.PathEscape(name), nil, &r)
	return r, err
}

func mediaFields(m MediaOptions) map[string]string {
	return map[string]string{
		"start": strconv.FormatFloat(m.Start, 'f', -1, 64),
		"end":   strconv.FormatFloat(m.End, 'f', -1, 64),
		"width": strconv.Itoa(m.Width), "height": strconv.Itoa(m.Height),
		"resize": m.Resize, "fps": strconv.Itoa(m.FPS), "quality": m.Quality,
		"crop_x": strconv.Itoa(m.CropX), "crop_y": strconv.Itoa(m.CropY),
		"crop_w": strconv.Itoa(m.CropW), "crop_h": strconv.Itoa(m.CropH),
	}
}

func uploadFields(u Upload) map[string]string {
	fields := mediaFields(u.MediaOptions)
	fields["emoji"], fields["fit"] = u.Emoji, u.Fit
	return fields
}

// CreateSet 用 file 作为第一张贴纸新建贴纸包
func (c *Client) CreateSet(ctx context.Context, cs CreateSet, file string) (s Set, err error) {
	fields := uploadFields(cs.Upload)
	fields["title"], fields["short_name"] = cs.Title, cs.ShortName
	err = c.upload(ctx, "/api/sets", fields, file, &s)
	return s, err
}

// AddSticker 把 file 转换后加到贴纸包末尾
func (c *Client) AddSticker(ctx context.Context, shortName string, u Upload, file string) (s Set, err error) {
	err = c.upload(ctx, "/api/sets/"+url.PathEscape(shortName)+"/stickers", uploadFields(u), file, &s)
	return s, err
}

func (c *Client) PatchSet(ctx context.Context, shortName string, p SetPatch) (s Set, err error) {
	err = c.do(ctx, http.MethodPatch, "/api/sets/"+url.PathEscape(shortName), p, &s)
	return s, err
}

func (c *Client) DeleteSet(ctx context.Context, shortName string) error {
	return c.do(ctx, http.MethodDelete, "/api/sets/"+url.PathEscape(shortName), nil, nil)
}

func stickerPath(id int64) string { return "/api/stickers/" + strconv.FormatInt(id, 10) }

func (c *Client) PatchSticker(ctx context.Context, id int64, p StickerPatch) (s Set, err error) {
	err = c.do(ctx, http.MethodPatch, stickerPath(id), p, &s)
	return s, err
}

func (c *Client) DeleteSticker(ctx context.Context, id int64) (s Set, err error) {
	err = c.do(ctx, http.MethodDelete, stickerPath(id), nil, &s)
	return s, err
}

// StickerThumb 返回贴纸的缩略图（WEBP、JPEG 或 PNG）
func (c *Client) StickerThumb(ctx context.Context, id int64) (b []byte, err error) {
	err = c.do(ctx, http.MethodGet, stickerPath(id)+"/thumb", nil, &b)
	return b, err
}

// GIFs 列出「GIF 收藏」
func (c *Client) GIFs(ctx context.Context) (g []GIF, err error) {
	err = c.do(ctx, http.MethodGet, "/api/gifs", nil, &g)
	return g, err
}

// AddGIF 把 file 转换成 Telegram 的 GIF（无声 MP4）存进「GIF 收藏」
func (c *Client) AddGIF(ctx context.Context, file string, opts MediaOptions) (g []GIF, err error) {
	err = c.upload(ctx, "/api/gifs", mediaFields(opts), file, &g)
	return g, err
}

func (c *Client) DeleteGIF(ctx context.Context, id int64) (g []GIF, err error) {
	err = c.do(ctx, http.MethodDelete, "/api/gifs/"+strconv.FormatInt(id, 10), nil, &g)
	return g, err
}

func (c *Client) GIFThumb(ctx context.Context, id int64) (b []byte, err error) {
	err = c.do(ctx, http.MethodGet, "/api/gifs/"+strconv.FormatInt(id, 10)+"/thumb", nil, &b)
	return b, err
}
