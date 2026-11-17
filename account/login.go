package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/VOfficial404/tg-stickers/api"
)

type codeState struct {
	via, notice, next string
	sent              bool
	retryAt           time.Time
}

type loginOperation struct {
	client   *telegram.Client
	ctx      context.Context
	connGen  uint64
	loginGen uint64
	finish   func()
}

// resetLoginLocked 让旧请求失效；旧 RPC 或二维码回调不得再覆盖新的登录步骤。
func (a *Account) resetLoginLocked() {
	a.loginGen++
	if a.qrCancel != nil {
		a.qrCancel()
		a.qrCancel = nil
	}
	if a.loginCancel != nil {
		a.loginCancel()
		a.loginCancel = nil
	}
	a.busy = false
	a.phone, a.codeHash, a.passwordHint, a.loginErr = "", "", "", ""
	a.code, a.qr = codeState{}, nil
}

func (a *Account) currentLogin(connGen, loginGen uint64) bool {
	return a.connGen == connGen && a.loginGen == loginGen && a.client != nil
}

// beginLogin 串行化短登录请求，HTTP 断开或 Telegram 连接结束都会取消它。
func (a *Account) beginLogin(ctx context.Context, step string) (loginOperation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil || a.connCtx == nil {
		return loginOperation{}, unavailable("正在连接 Telegram，稍后再试")
	}
	if a.step != step {
		return loginOperation{}, userError("登录步骤已改变，请刷新页面后再试")
	}
	if a.busy {
		return loginOperation{}, userError("上一个登录请求还在进行，请稍候")
	}
	a.busy, a.loginErr = true, ""
	rpcCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.connCtx, cancel)
	a.loginCancel = cancel
	op := loginOperation{client: a.client, ctx: rpcCtx, connGen: a.connGen, loginGen: a.loginGen}
	op.finish = func() {
		stop()
		cancel()
		a.mu.Lock()
		if a.currentLogin(op.connGen, op.loginGen) {
			a.busy, a.loginCancel = false, nil
		}
		a.mu.Unlock()
	}
	return op, nil
}

// normalizePhone 保留国际号码中的数字，只接受常见排版字符，不把拼错的字母静默丢掉。
func normalizePhone(phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	var digits strings.Builder
	for i, r := range phone {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '+' && i == 0:
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return "", userError("手机号只能包含数字、开头的 + 和空格、括号、横线；请使用半角数字")
		}
	}
	out := digits.String()
	if len(out) < 7 || len(out) > 15 || out[0] == '0' {
		return "", userError("请填写国际号码：国家码加手机号。美国是 +1 后接 10 位号码，不要加 001")
	}
	if out[0] == '1' && len(out) != 11 {
		return "", userError("美国/加拿大号码应为 +1 后接 10 位号码（包含 3 位区号）")
	}
	return out, nil
}

// SendCode 只开始一次新请求。重发必须使用 ResendCode 和当前 phone_code_hash。
func (a *Account) SendCode(ctx context.Context, phone string) error {
	phone, err := normalizePhone(phone)
	if err != nil {
		return err
	}
	op, err := a.beginLogin(ctx, api.StepPhone)
	if err != nil {
		return err
	}
	defer op.finish()
	sent, err := op.client.Auth().SendCode(op.ctx, phone, auth.SendCodeOptions{})
	if err != nil {
		if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
			return a.needPassword(op.ctx, op.client, op.connGen, op.loginGen)
		}
		return a.loginError("sendCode", err)
	}
	return a.acceptSentCode(op, phone, sent)
}

// ResendCode 沿 Telegram 的 next_type 走，不重新发起 auth.sendCode，不绕过等待时间。
func (a *Account) ResendCode(ctx context.Context) error {
	op, err := a.beginLogin(ctx, api.StepCode)
	if err != nil {
		return err
	}
	defer op.finish()
	a.mu.Lock()
	phone, hash, code := a.phone, a.codeHash, a.code
	a.mu.Unlock()
	if code.next == "" {
		return userError("Telegram 没有提供下一种发送方式。可在已登录的手机 Telegram 中查看，或改用扫码登录")
	}
	if left := time.Until(code.retryAt); left > 0 {
		return &Error{Status: 429, Message: fmt.Sprintf("Telegram 要求再等 %d 秒才能重发", int(left.Seconds())+1)}
	}
	sent, err := op.client.Auth().ResendCode(op.ctx, phone, hash)
	if err != nil {
		if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
			return a.needPassword(op.ctx, op.client, op.connGen, op.loginGen)
		}
		return a.loginError("resendCode", err)
	}
	return a.acceptSentCode(op, phone, sent)
}

func (a *Account) acceptSentCode(op loginOperation, phone string, sent tg.AuthSentCodeClass) error {
	if success, ok := sent.(*tg.AuthSentCodeSuccess); ok {
		authz, ok := success.Authorization.(*tg.AuthAuthorization)
		if !ok {
			return userError("这个号码尚未注册，请先使用官方手机 Telegram 注册")
		}
		return a.authorized(op.connGen, op.loginGen, authz)
	}
	code, hash, err := describeSentCode(sent, time.Now())
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.currentLogin(op.connGen, op.loginGen) {
		return context.Canceled
	}
	a.step, a.phone, a.codeHash, a.code = api.StepCode, phone, hash, code
	// 只记录类型和等待时间，不记录手机号、hash、验证码或二维码令牌。
	a.cfg.Logf("Telegram 验证码响应：constructor=%T channel=%s sent=%t next=%s wait=%ds", sent, code.via, code.sent, code.next, max(0, int(time.Until(code.retryAt).Seconds())))
	return nil
}

func nextCodeType(t tg.AuthCodeTypeClass) string {
	switch t.(type) {
	case *tg.AuthCodeTypeSMS:
		return "短信"
	case *tg.AuthCodeTypeCall:
		return "电话语音"
	case *tg.AuthCodeTypeFlashCall:
		return "闪电来电"
	case *tg.AuthCodeTypeMissedCall:
		return "未接来电"
	case *tg.AuthCodeTypeFragmentSMS:
		return "Fragment"
	}
	return ""
}

// describeSentCode 区分“已选择发送渠道”与需要官方 App / 邮箱设置的额外步骤。
func describeSentCode(sent tg.AuthSentCodeClass, now time.Time) (codeState, string, error) {
	if payment, ok := sent.(*tg.AuthSentCodePaymentRequired); ok {
		return codeState{via: "官方 App 的付费验证", notice: "Telegram 要求先在官方手机 App 完成付费验证，此程序不能处理这项支付。手机已登录时可改用扫码登录。"}, payment.PhoneCodeHash, nil
	}
	s, ok := sent.(*tg.AuthSentCode)
	if !ok {
		return codeState{}, "", userError(fmt.Sprintf("Telegram 返回了未支持的验证方式（%T），请使用官方手机 App 或扫码登录", sent))
	}
	out := codeState{sent: true}
	switch t := s.Type.(type) {
	case *tg.AuthSentCodeTypeApp:
		out.via = "已登录的 Telegram App"
		out.notice = "本次不是短信。请在已登录同一手机号的 Telegram 中查看名为「Telegram」的官方服务消息（也检查归档）。"
	case *tg.AuthSentCodeTypeSMS:
		out.via = "短信"
	case *tg.AuthSentCodeTypeSMSWord:
		out.via, out.notice = "短信单词", "验证码是短信中的一个单词，请原样填写。"
	case *tg.AuthSentCodeTypeSMSPhrase:
		out.via, out.notice = "短信短语", "验证码是短信中的短语，请保留单词间的空格。"
	case *tg.AuthSentCodeTypeCall:
		out.via = "电话语音"
	case *tg.AuthSentCodeTypeFlashCall:
		out.via, out.notice = "闪电来电", "验证码是来电号码本身，不是号码末尾几位。"
	case *tg.AuthSentCodeTypeMissedCall:
		out.via, out.notice = "未接来电", "请输入来电号码的最后几位。"
	case *tg.AuthSentCodeTypeEmailCode:
		out.via, out.notice = "登录邮箱", "验证码已发送到登录邮箱："+t.EmailPattern
	case *tg.AuthSentCodeTypeFragmentSMS:
		out.via, out.notice = "Fragment", "请在 Fragment 查看验证码："+t.URL
	case *tg.AuthSentCodeTypeFirebaseSMS:
		out.sent, out.via = false, "官方手机 App 的 Firebase 验证"
		out.notice = "这个渠道还需要官方手机 App 的设备验证，短信尚未由此程序请求发出。等待后尝试 Telegram 提供的下一渠道，或改用扫码登录。"
	case *tg.AuthSentCodeTypeSetUpEmailRequired:
		out.sent, out.via = false, "设置登录邮箱"
		out.notice = "Telegram 要求先设置登录邮箱，尚未发送验证码。请在官方手机 App 完成邮箱验证，或用已登录的手机扫码登录。"
	default:
		out.sent, out.via = false, "未支持的验证渠道"
		out.notice = "Telegram 返回了本程序不支持的验证方式，请使用官方手机 App 或扫码登录。"
	}
	out.next = nextCodeType(s.NextType)
	if out.next != "" && s.Timeout > 0 {
		out.retryAt = now.Add(time.Duration(s.Timeout) * time.Second)
	}
	return out, s.PhoneCodeHash, nil
}

func (a *Account) SignIn(ctx context.Context, code string) error {
	op, err := a.beginLogin(ctx, api.StepCode)
	if err != nil {
		return err
	}
	defer op.finish()
	a.mu.Lock()
	phone, hash, sent := a.phone, a.codeHash, a.code.sent
	a.mu.Unlock()
	if !sent {
		return userError("当前渠道尚未发送可用验证码，请按页面提示操作或改用扫码登录")
	}
	authz, err := op.client.Auth().SignIn(op.ctx, phone, strings.TrimSpace(code), hash)
	if errors.Is(err, auth.ErrPasswordAuthNeeded) {
		return a.needPassword(op.ctx, op.client, op.connGen, op.loginGen)
	}
	var signup *auth.SignUpRequired
	if errors.As(err, &signup) {
		return userError("这个号码尚未注册 Telegram，请先使用官方手机 App 注册")
	}
	if err != nil {
		return a.loginError("signIn", err)
	}
	return a.authorized(op.connGen, op.loginGen, authz)
}

func (a *Account) needPassword(ctx context.Context, client *telegram.Client, connGen, loginGen uint64) error {
	hint := ""
	if p, err := client.API().AccountGetPassword(ctx); err == nil {
		hint = p.Hint
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.currentLogin(connGen, loginGen) {
		return context.Canceled
	}
	a.step, a.passwordHint, a.qr, a.loginErr = api.StepPassword, hint, nil, ""
	return nil
}

func (a *Account) Password(ctx context.Context, password string) error {
	op, err := a.beginLogin(ctx, api.StepPassword)
	if err != nil {
		return err
	}
	defer op.finish()
	authz, err := op.client.Auth().Password(op.ctx, password)
	if errors.Is(err, auth.ErrPasswordInvalid) {
		return userError("两步验证密码不对")
	}
	if err != nil {
		return a.loginError("password", err)
	}
	return a.authorized(op.connGen, op.loginGen, authz)
}

func (a *Account) authorized(connGen, loginGen uint64, authz *tg.AuthAuthorization) error {
	if authz == nil {
		return userError("Telegram 没有返回登录结果")
	}
	user, ok := authz.User.(*tg.User)
	if !ok {
		return userError("Telegram 没有返回有效的用户信息")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.currentLogin(connGen, loginGen) {
		return context.Canceled
	}
	a.resetLoginLocked()
	a.step, a.user = api.StepReady, toUser(user)
	a.cfg.Logf("Telegram 用户账号登录成功")
	return nil
}

// StartQR 绑定现有连接、响应手机授权推送并处理跨 DC 登录；整个扫码过程最多五分钟。
func (a *Account) StartQR() error {
	a.mu.Lock()
	if a.client == nil || a.connCtx == nil {
		a.mu.Unlock()
		return unavailable("正在连接 Telegram，稍后再试")
	}
	if a.step == api.StepReady || a.busy {
		a.mu.Unlock()
		return userError("已经登录或有登录请求正在进行，请稍候")
	}
	a.resetLoginLocked()
	a.step = api.StepQR
	ctx, cancel := context.WithTimeout(a.connCtx, 5*time.Minute)
	a.qrCancel = cancel
	client, signal, connGen, loginGen := a.client, a.loggedIn, a.connGen, a.loginGen
	// 丢掉上次扫码遗留的通知，新 token 必须由本次授权确认。
	for {
		select {
		case <-signal:
		default:
			a.mu.Unlock()
			go a.runQR(ctx, cancel, client, signal, connGen, loginGen)
			return nil
		}
	}
}

func (a *Account) runQR(ctx context.Context, cancel context.CancelFunc, client *telegram.Client, signal qrlogin.LoggedIn, connGen, loginGen uint64) {
	defer cancel()
	authz, err := client.QR().Auth(ctx, signal, func(ctx context.Context, token qrlogin.Token) error {
		a.mu.Lock()
		defer a.mu.Unlock()
		if !a.currentLogin(connGen, loginGen) || a.step != api.StepQR {
			return context.Canceled
		}
		a.qr = &api.LoginQR{URL: token.URL(), Expires: token.Expires().Unix()}
		return nil
	})
	if err == nil {
		err = a.authorized(connGen, loginGen, authz)
	} else if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
		err = a.needPassword(ctx, client, connGen, loginGen)
	}
	if err == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.currentLogin(connGen, loginGen) {
		return
	}
	a.qr, a.qrCancel = nil, nil
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		a.loginErr = "扫码登录已超时，请重新生成二维码"
	} else {
		a.loginErr = humanize(err).Error()
	}
	a.cfg.Logf("Telegram 扫码登录失败：%s", authErrorType(err))
}

// CancelLogin 取消后台扫码与尚未完成的登录请求，并使它们的回调失效。
func (a *Account) CancelLogin() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.step == api.StepCode || a.step == api.StepPassword || a.step == api.StepQR || a.step == api.StepPhone {
		a.resetLoginLocked()
		a.step = api.StepPhone
	}
}

func authErrorType(err error) string {
	if rpc, ok := tgerr.As(err); ok {
		return rpc.Type
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "TIMEOUT"
	}
	return fmt.Sprintf("%T", err)
}

func (a *Account) loginError(method string, err error) error {
	a.cfg.Logf("Telegram 登录请求失败：method=%s type=%s", method, authErrorType(err))
	return humanize(err)
}
