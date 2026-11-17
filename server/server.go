// Package server 是给客户端的 HTTP 接口：令牌校验、上传、转换，Telegram 上的事交给 account。
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/VOfficial404/tg-stickers/account"
	"github.com/VOfficial404/tg-stickers/api"
	"github.com/VOfficial404/tg-stickers/convert"
	"github.com/VOfficial404/tg-stickers/web"
)

// MaxUploadBytes 是一个上传文件的上限
const MaxUploadBytes = 50 << 20

type Server struct {
	acc     *account.Account
	ff      *convert.Tool // 没找到 ffmpeg 时是 nil
	ffVer   string
	ffErr   string
	token   string
	version string
	logf    func(string, ...any)
	// convertSlots 限制同时运行的 ffmpeg 个数，VP9 编码很吃 CPU
	convertSlots chan struct{}
}

type Config struct {
	Account *account.Account
	FFmpeg  string // ffmpeg 的路径，空表示在 PATH 里找
	DataDir string
	Token   string // 空表示用数据目录里保存的，没有就生成一个
	Version string
	Logf    func(string, ...any)
}

func New(cfg Config) (*Server, error) {
	s := &Server{acc: cfg.Account, version: cfg.Version, logf: cfg.Logf, convertSlots: make(chan struct{}, 2)}
	if t, err := convert.Find(cfg.FFmpeg); err != nil {
		s.ffErr = err.Error()
	} else {
		s.ff = &t
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.ffVer = t.Version(ctx)
		cancel()
	}
	tok, err := LoadToken(cfg.DataDir, cfg.Token)
	if err != nil {
		return nil, err
	}
	s.token = tok
	return s, nil
}

// FFmpeg 返回 ffmpeg 版本；找不到时返回空和原因
func (s *Server) FFmpeg() (string, string) { return s.ffVer, s.ffErr }

// LoadToken 返回接口令牌：env 不为空时用它，否则读数据目录里的 token，没有就生成一个存下
func LoadToken(dataDir, env string) (string, error) {
	if t := strings.TrimSpace(env); t != "" {
		return t, nil
	}
	path := filepath.Join(dataDir, "token")
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t := hex.EncodeToString(b)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("保存令牌失败：%w", err)
	}
	return t, nil
}

func (s *Server) Token() string { return s.token }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", web.Handler())
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, api.Ping{Name: "tg-stickers", Version: s.version})
	})
	auth := func(pattern string, h func(http.ResponseWriter, *http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if !s.authorized(r) {
				writeError(w, http.StatusUnauthorized, "令牌不对")
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/login") || r.URL.Path == "/api/state" {
				w.Header().Set("Cache-Control", "no-store")
			}
			out, err := h(w, r)
			if err != nil {
				status, msg := http.StatusInternalServerError, err.Error()
				var e *account.Error
				if errors.As(err, &e) {
					status = e.Status
				}
				if status >= 500 {
					s.logf("%s %s：%s", r.Method, r.URL.Path, msg)
				}
				writeError(w, status, msg)
				return
			}
			switch out := out.(type) {
			case nil:
				writeJSON(w, struct{}{})
			case []byte:
				w.Header().Set("Content-Type", http.DetectContentType(out))
				if w.Header().Get("Cache-Control") == "" {
					w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
				}
				w.Write(out)
			default:
				writeJSON(w, out)
			}
		})
	}

	auth("GET /api/state", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.state(), nil })
	auth("PUT /api/credentials", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var c api.Credentials
		if err := readJSON(r, &c); err != nil {
			return nil, err
		}
		if err := s.acc.SetCredentials(c); err != nil {
			return nil, err
		}
		return s.state(), nil
	})
	auth("POST /api/login/phone", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var p api.PhoneRequest
		if err := readJSON(r, &p); err != nil {
			return nil, err
		}
		return s.stateAfter(s.acc.SendCode(r.Context(), p.Phone))
	})
	auth("POST /api/login/resend", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.stateAfter(s.acc.ResendCode(r.Context()))
	})
	auth("POST /api/login/qr", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.stateAfter(s.acc.StartQR())
	})
	auth("GET /api/login/qr.png", func(w http.ResponseWriter, r *http.Request) (any, error) {
		st := s.acc.State()
		if st.Step != api.StepQR || st.QR == nil || st.QR.Expires <= time.Now().Unix() {
			return nil, &account.Error{Status: http.StatusConflict, Message: "二维码正在生成或已过期，请稍后刷新"}
		}
		code, err := qr.Encode(st.QR.URL, qr.M)
		if err != nil {
			return nil, err
		}
		code.Scale = 6
		return code.PNG(), nil
	})
	auth("POST /api/login/code", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var p api.CodeRequest
		if err := readJSON(r, &p); err != nil {
			return nil, err
		}
		return s.stateAfter(s.acc.SignIn(r.Context(), p.Code))
	})
	auth("POST /api/login/password", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var p api.PasswordRequest
		if err := readJSON(r, &p); err != nil {
			return nil, err
		}
		return s.stateAfter(s.acc.Password(r.Context(), p.Password))
	})
	auth("DELETE /api/login", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s.acc.CancelLogin()
		return s.state(), nil
	})
	auth("POST /api/logout", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.stateAfter(s.acc.Logout(r.Context()))
	})

	auth("GET /api/sets", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.acc.Sets(r.Context()) })
	auth("POST /api/sets", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var out api.Set
		err := s.withUpload(w, r, func(path string, form map[string]string) error {
			if form["title"] == "" || form["short_name"] == "" {
				return badRequest("请填写贴纸包名字和链接名")
			}
			opts, err := mediaOptions(form, true)
			if err != nil {
				return err
			}
			st, tmp, err := s.sticker(r.Context(), path, opts)
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)
			out, err = s.acc.CreateSet(r.Context(), form["title"], form["short_name"], form["emoji"], st)
			return err
		})
		return out, err
	})
	auth("GET /api/sets/{name}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.acc.Set(r.Context(), r.PathValue("name"))
	})
	auth("PATCH /api/sets/{name}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var p api.SetPatch
		if err := readJSON(r, &p); err != nil {
			return nil, err
		}
		return s.acc.RenameSet(r.Context(), r.PathValue("name"), p.Title)
	})
	auth("DELETE /api/sets/{name}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return nil, s.acc.DeleteSet(r.Context(), r.PathValue("name"))
	})
	auth("POST /api/sets/{name}/stickers", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var out api.Set
		err := s.withUpload(w, r, func(path string, form map[string]string) error {
			opts, err := mediaOptions(form, true)
			if err != nil {
				return err
			}
			st, tmp, err := s.sticker(r.Context(), path, opts)
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)
			out, err = s.acc.AddSticker(r.Context(), r.PathValue("name"), form["emoji"], st)
			return err
		})
		return out, err
	})
	auth("GET /api/short-names/{name}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.acc.CheckShortName(r.Context(), r.PathValue("name"))
	})

	auth("PATCH /api/stickers/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		var p api.StickerPatch
		if err := readJSON(r, &p); err != nil {
			return nil, err
		}
		return s.acc.PatchSticker(r.Context(), id, p)
	})
	auth("DELETE /api/stickers/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.acc.DeleteSticker(r.Context(), id)
	})
	thumb := func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.acc.Thumb(r.Context(), id, s.ff)
	}
	auth("GET /api/stickers/{id}/thumb", thumb)
	auth("GET /api/gifs/{id}/thumb", thumb)

	auth("GET /api/gifs", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.acc.GIFs(r.Context()) })
	auth("POST /api/gifs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var out []api.GIF
		err := s.withUpload(w, r, func(path string, form map[string]string) error {
			opts, err := mediaOptions(form, false)
			if err != nil {
				return err
			}
			ff, err := s.ffmpeg()
			if err != nil {
				return err
			}
			tmp, err := os.MkdirTemp("", "tg-stickers-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)
			release := s.slot(r.Context())
			g, err := ff.GIF(r.Context(), path, tmp, opts.MediaOptions)
			release()
			if err != nil {
				return badRequest(err.Error())
			}
			out, err = s.acc.AddGIF(r.Context(), g)
			return err
		})
		return out, err
	})
	auth("DELETE /api/gifs/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.acc.DeleteGIF(r.Context(), id)
	})
	return mux
}

func (s *Server) authorized(r *http.Request) bool {
	t, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1
}

func (s *Server) state() api.State {
	st := s.acc.State()
	st.Version = s.version
	st.FFmpeg = s.ffVer
	st.Limits = api.Limits{MaxUploadBytes: MaxUploadBytes}
	return st
}

// stateAfter 在登录的一步之后返回新状态；出错时返回错误
func (s *Server) stateAfter(err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return s.state(), nil
}

func (s *Server) ffmpeg() (*convert.Tool, error) {
	if s.ff == nil {
		return nil, &account.Error{Status: http.StatusServiceUnavailable, Message: s.ffErr}
	}
	return s.ff, nil
}

// slot 等一个 ffmpeg 名额，返回归还名额的函数
func (s *Server) slot(ctx context.Context) func() {
	select {
	case s.convertSlots <- struct{}{}:
		return func() { <-s.convertSlots }
	case <-ctx.Done():
		return func() {}
	}
}

// sticker 把上传的文件转换成贴纸，返回转换结果和要删掉的临时目录
func (s *Server) sticker(ctx context.Context, path string, opts api.Upload) (convert.Sticker, string, error) {
	ff, err := s.ffmpeg()
	if err != nil {
		return convert.Sticker{}, "", err
	}
	tmp, err := os.MkdirTemp("", "tg-stickers-")
	if err != nil {
		return convert.Sticker{}, "", err
	}
	release := s.slot(ctx)
	defer release()
	if err := ctx.Err(); err != nil {
		os.RemoveAll(tmp)
		return convert.Sticker{}, "", err
	}
	st, err := ff.Sticker(ctx, path, tmp, opts)
	if err != nil {
		os.RemoveAll(tmp)
		return convert.Sticker{}, "", badRequest(err.Error())
	}
	return st, tmp, nil
}

// withUpload 把 multipart 里的 file 存到临时文件，连同其他字段交给 fn
func (s *Server) withUpload(w http.ResponseWriter, r *http.Request, fn func(path string, form map[string]string) error) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		return badRequest("上传的格式不对")
	}
	form := map[string]string{}
	path := ""
	defer func() {
		if path != "" {
			os.Remove(path)
		}
	}()
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return uploadError(err)
		}
		if part.FormName() == "file" {
			f, err := os.CreateTemp("", "tg-stickers-upload-*"+filepath.Ext(part.FileName()))
			if err != nil {
				return err
			}
			path = f.Name()
			n, err := io.Copy(f, io.LimitReader(part, MaxUploadBytes+1))
			f.Close()
			if err != nil {
				return uploadError(err)
			}
			if n > MaxUploadBytes {
				return badRequest(fmt.Sprintf("文件超过 %d MB", MaxUploadBytes>>20))
			}
			continue
		}
		b, err := io.ReadAll(io.LimitReader(part, 4<<10))
		if err != nil {
			return uploadError(err)
		}
		form[part.FormName()] = string(b)
	}
	if path == "" {
		return badRequest("没有收到文件")
	}
	return fn(path, form)
}

func uploadError(err error) error {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return badRequest(fmt.Sprintf("文件超过 %d MB", MaxUploadBytes>>20))
	}
	return badRequest("接收上传的文件失败：" + err.Error())
}

func badRequest(msg string) error {
	return &account.Error{Status: http.StatusBadRequest, Message: msg}
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, badRequest("ID 不对")
	}
	return id, nil
}

func readJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return badRequest("请求的内容看不懂")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(api.Error{Message: msg})
}
