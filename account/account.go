// Package account 用你自己的 Telegram 账号（MTProto，不是 Bot）登录，管理账号创建的贴纸包和 GIF 收藏。
package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/tg"
	"golang.org/x/net/proxy"

	"github.com/VOfficial404/tg-stickers/api"
)

// Config 是账号的配置
type Config struct {
	DataDir string
	// Credentials 来自环境变量；为零值时用客户端里填的、保存在数据目录里的
	Credentials api.Credentials
	// Proxy 是连接 Telegram 用的代理，例如 socks5://127.0.0.1:1080
	Proxy   string
	Version string
	Logf    func(format string, args ...any)
}

// Account 维持和 Telegram 的连接，记录登录进行到哪一步
type Account struct {
	cfg Config

	mu           sync.Mutex
	creds        api.Credentials
	step         string
	codeVia      string
	passwordHint string
	phone        string
	codeHash     string
	user         *api.User
	lastErr      string
	client       *telegram.Client
	stop         context.CancelFunc
	done         chan struct{}

	docsMu sync.Mutex
	docs   map[int64]docRef
}

// docRef 记住见过的文件：改贴纸、取缩略图都要它的 access_hash 和 file_reference
type docRef struct {
	doc *tg.Document
	set string // 所在贴纸包的链接名；GIF 收藏里的是 ""
}

// New 读出保存的 api_id / api_hash；Start 之后才连接 Telegram
func New(cfg Config) (*Account, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	a := &Account{cfg: cfg, docs: map[int64]docRef{}, step: api.StepCredentials}
	if a.credentialsFromEnv() {
		a.creds = cfg.Credentials
	} else if b, err := os.ReadFile(a.credentialsPath()); err == nil {
		if err := json.Unmarshal(b, &a.creds); err != nil {
			return nil, fmt.Errorf("读取 %s 失败：%w", a.credentialsPath(), err)
		}
	}
	return a, nil
}

func (a *Account) credentialsFromEnv() bool {
	return a.cfg.Credentials.APIID != 0 && a.cfg.Credentials.APIHash != ""
}
func (a *Account) credentialsPath() string { return filepath.Join(a.cfg.DataDir, "credentials.json") }
func (a *Account) sessionPath() string     { return filepath.Join(a.cfg.DataDir, "session.json") }

// Start 有 api_id / api_hash 时开始连接 Telegram
func (a *Account) Start() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.startLocked()
}

// Stop 断开 Telegram，等连接退出
func (a *Account) Stop() {
	a.mu.Lock()
	done := a.stopLocked()
	a.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (a *Account) startLocked() {
	if a.creds.APIID == 0 || a.creds.APIHash == "" {
		a.step = api.StepCredentials
		return
	}
	resolver, err := a.resolver()
	if err != nil {
		a.step, a.lastErr = api.StepConnecting, err.Error()
		return
	}
	a.step, a.lastErr = api.StepConnecting, ""
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.stop, a.done = cancel, done
	go a.run(ctx, done, resolver)
}

// stopLocked 让连接退出，返回它退出时关闭的 channel
func (a *Account) stopLocked() chan struct{} {
	if a.stop == nil {
		return nil
	}
	a.stop()
	done := a.done
	a.stop, a.done, a.client = nil, nil, nil
	return done
}

func (a *Account) resolver() (dcs.Resolver, error) {
	if a.cfg.Proxy == "" {
		return nil, nil
	}
	u, err := url.Parse(a.cfg.Proxy)
	if err != nil {
		return nil, fmt.Errorf("代理地址 %q 不对：%w", a.cfg.Proxy, err)
	}
	d, err := proxy.FromURL(u, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("代理地址 %q 不对，只支持 socks5://：%w", a.cfg.Proxy, err)
	}
	cd, ok := d.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("代理 %q 不支持", a.cfg.Proxy)
	}
	return dcs.Plain(dcs.PlainOptions{Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return cd.DialContext(ctx, network, addr)
	}}), nil
}

// run 保持连接；连不上时隔一会儿重试，直到 ctx 取消
func (a *Account) run(ctx context.Context, done chan struct{}, resolver dcs.Resolver) {
	defer close(done)
	opts := telegram.Options{
		SessionStorage: &session.FileStorage{Path: a.sessionPath()},
		NoUpdates:      true,
		Device: telegram.DeviceConfig{
			DeviceModel:   "tg-stickers",
			SystemVersion: "server",
			AppVersion:    a.cfg.Version,
		},
	}
	if resolver != nil {
		opts.Resolver = resolver
	}
	for wait := 5 * time.Second; ; wait = min(wait*2, 5*time.Minute) {
		client := telegram.NewClient(a.creds.APIID, a.creds.APIHash, opts)
		err := client.Run(ctx, func(ctx context.Context) error {
			if err := a.connected(ctx, client, done); err != nil {
				return err
			}
			wait = 5 * time.Second
			<-ctx.Done()
			return ctx.Err()
		})
		if ctx.Err() != nil {
			return
		}
		msg := humanize(err).Error()
		a.cfg.Logf("连接 Telegram 出错，%s 后重试：%s", wait, msg)
		a.mu.Lock()
		if a.done == done {
			a.client, a.step, a.lastErr = nil, api.StepConnecting, msg
		}
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// connected 在连上之后查登录状态；done 不是当前连接的（已经换了 api_id）就不动状态
func (a *Account) connected(ctx context.Context, client *telegram.Client, done chan struct{}) error {
	st, err := client.Auth().Status(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done != done {
		return nil
	}
	a.client, a.lastErr = client, ""
	if st.Authorized {
		a.step, a.user = api.StepReady, toUser(st.User)
		a.cfg.Logf("已登录 Telegram：%s", a.user.Name)
	} else if a.step != api.StepCode && a.step != api.StepPassword {
		a.step, a.user = api.StepPhone, nil
		a.cfg.Logf("已连上 Telegram，等待登录")
	}
	return nil
}

func toUser(u *tg.User) *api.User {
	if u == nil {
		return nil
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	return &api.User{ID: u.ID, Name: name, Username: u.Username, Phone: u.Phone}
}

// State 返回当前状态，ffmpeg 和上传限制由调用的人填
func (a *Account) State() api.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := api.State{Step: a.step, Error: a.lastErr, CredentialsFromEnv: a.credentialsFromEnv()}
	switch a.step {
	case api.StepCode:
		s.CodeVia = a.codeVia
	case api.StepPassword:
		s.PasswordHint = a.passwordHint
	case api.StepReady:
		s.User = a.user
	}
	return s
}

// SetCredentials 保存 api_id / api_hash 并用它们重新连接
func (a *Account) SetCredentials(c api.Credentials) error {
	c.APIHash = strings.TrimSpace(c.APIHash)
	if c.APIID <= 0 || len(c.APIHash) != 32 {
		return userError("api_id 是一串数字，api_hash 是 32 位的字母数字，都在 my.telegram.org 的 API development tools 里")
	}
	a.mu.Lock()
	if a.credentialsFromEnv() {
		a.mu.Unlock()
		return userError("api_id / api_hash 由服务端的环境变量指定，要改请改环境变量")
	}
	if a.step == api.StepReady {
		a.mu.Unlock()
		return userError("已经登录了，先退出登录再换 api_id / api_hash")
	}
	b, _ := json.Marshal(c)
	if err := os.WriteFile(a.credentialsPath(), b, 0o600); err != nil {
		a.mu.Unlock()
		return err
	}
	a.creds = c
	done := a.stopLocked()
	a.mu.Unlock()
	if done != nil {
		<-done
	}
	// 换了应用，旧会话不能再用
	_ = os.Remove(a.sessionPath())
	a.Start()
	return nil
}

// ready 返回已登录的连接
func (a *Account) ready() (*telegram.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		if a.lastErr != "" {
			return nil, unavailable("还没连上 Telegram：" + a.lastErr)
		}
		return nil, unavailable("正在连接 Telegram，稍后再试")
	}
	if a.step != api.StepReady {
		return nil, unauthorized("还没有登录 Telegram 账号")
	}
	return a.client, nil
}

// loginClient 返回还没登录的连接
func (a *Account) loginClient(steps ...string) (*telegram.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		if a.step == api.StepCredentials {
			return nil, userError("先填写 api_id 和 api_hash")
		}
		return nil, unavailable("正在连接 Telegram，稍后再试")
	}
	for _, s := range steps {
		if a.step == s {
			return a.client, nil
		}
	}
	if a.step == api.StepReady {
		return nil, userError("已经登录了")
	}
	return nil, userError("登录步骤对不上，刷新一下重新来")
}

// SendCode 给手机号发验证码
func (a *Account) SendCode(ctx context.Context, phone string) error {
	phone = normalizePhone(phone)
	if len(phone) < 6 {
		return userError("手机号要带国家码，例如 +8613800000000")
	}
	client, err := a.loginClient(api.StepPhone, api.StepCode, api.StepPassword)
	if err != nil {
		return err
	}
	sent, err := client.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})
	if err != nil {
		return humanize(err)
	}
	switch s := sent.(type) {
	case *tg.AuthSentCode:
		a.mu.Lock()
		a.step, a.phone, a.codeHash, a.codeVia = api.StepCode, phone, s.PhoneCodeHash, codeVia(s.Type)
		a.mu.Unlock()
		return nil
	case *tg.AuthSentCodeSuccess:
		return a.signedIn(ctx, client)
	default:
		return fmt.Errorf("Telegram 的回复看不懂：%T", sent)
	}
}

func normalizePhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func codeVia(t tg.AuthSentCodeTypeClass) string {
	switch t.(type) {
	case *tg.AuthSentCodeTypeApp:
		return "已登录的 Telegram App（看「Telegram」官方账号发来的消息）"
	case *tg.AuthSentCodeTypeSMS, *tg.AuthSentCodeTypeSMSWord, *tg.AuthSentCodeTypeSMSPhrase, *tg.AuthSentCodeTypeFirebaseSMS:
		return "短信"
	case *tg.AuthSentCodeTypeCall:
		return "电话语音"
	case *tg.AuthSentCodeTypeFlashCall, *tg.AuthSentCodeTypeMissedCall:
		return "未接来电（号码的最后几位就是验证码）"
	case *tg.AuthSentCodeTypeEmailCode:
		return "邮箱"
	case *tg.AuthSentCodeTypeFragmentSMS:
		return "Fragment"
	}
	return "Telegram"
}

// SignIn 提交验证码
func (a *Account) SignIn(ctx context.Context, code string) error {
	code = strings.TrimSpace(code)
	client, err := a.loginClient(api.StepCode)
	if err != nil {
		return err
	}
	a.mu.Lock()
	phone, hash := a.phone, a.codeHash
	a.mu.Unlock()
	_, err = client.Auth().SignIn(ctx, phone, code, hash)
	if errors.Is(err, auth.ErrPasswordAuthNeeded) {
		hint := ""
		if p, err := client.API().AccountGetPassword(ctx); err == nil {
			hint = p.Hint
		}
		a.mu.Lock()
		a.step, a.passwordHint = api.StepPassword, hint
		a.mu.Unlock()
		return nil
	}
	var signUp *auth.SignUpRequired
	if errors.As(err, &signUp) {
		return userError("这个手机号还没有注册 Telegram，先在 Telegram App 里注册")
	}
	if err != nil {
		return humanize(err)
	}
	return a.signedIn(ctx, client)
}

// Password 提交两步验证密码
func (a *Account) Password(ctx context.Context, password string) error {
	client, err := a.loginClient(api.StepPassword)
	if err != nil {
		return err
	}
	if _, err := client.Auth().Password(ctx, password); err != nil {
		if errors.Is(err, auth.ErrPasswordInvalid) {
			return userError("两步验证密码不对")
		}
		return humanize(err)
	}
	return a.signedIn(ctx, client)
}

func (a *Account) signedIn(ctx context.Context, client *telegram.Client) error {
	self, err := client.Self(ctx)
	if err != nil {
		return humanize(err)
	}
	a.mu.Lock()
	a.step, a.user, a.phone, a.codeHash, a.codeVia, a.passwordHint = api.StepReady, toUser(self), "", "", "", ""
	a.mu.Unlock()
	a.cfg.Logf("已登录 Telegram：%s", toUser(self).Name)
	return nil
}

// CancelLogin 放弃进行到一半的登录
func (a *Account) CancelLogin() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.step == api.StepCode || a.step == api.StepPassword {
		a.step, a.phone, a.codeHash, a.codeVia, a.passwordHint = api.StepPhone, "", "", "", ""
	}
}

// Logout 在 Telegram 上注销这次登录，删掉会话文件后重新连接
func (a *Account) Logout(ctx context.Context) error {
	client, err := a.ready()
	if err != nil {
		return err
	}
	if _, err := client.API().AuthLogOut(ctx); err != nil {
		a.cfg.Logf("在 Telegram 上注销失败，仍然删除本地会话：%v", err)
	}
	a.mu.Lock()
	done := a.stopLocked()
	a.user = nil
	a.mu.Unlock()
	if done != nil {
		<-done
	}
	if err := os.Remove(a.sessionPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	a.docsMu.Lock()
	a.docs = map[int64]docRef{}
	a.docsMu.Unlock()
	a.Start()
	return nil
}

// loggedOut 在操作发现会话已经失效（例如在手机上被踢掉）时回到填手机号
func (a *Account) loggedOut(err error) error {
	if isAuthLost(err) {
		a.mu.Lock()
		if a.step == api.StepReady {
			a.step, a.user = api.StepPhone, nil
			a.cfg.Logf("Telegram 登录已失效，需要重新登录")
		}
		a.mu.Unlock()
		return unauthorized("Telegram 登录已失效，重新登录一次")
	}
	return humanize(err)
}
