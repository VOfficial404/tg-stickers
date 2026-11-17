package account

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/VOfficial404/tg-stickers/api"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

func TestInternationalPhoneNormalization(t *testing.T) {
	for _, in := range []string{"+1 (202) 555-0123", "12025550123", "+1 202.555.0123"} {
		got, err := normalizePhone(in)
		if err != nil || got != "12025550123" {
			t.Errorf("normalizePhone(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"+1 202555012", "+1 20255501234", "+1 202ABC0123", "0012025550123", "+１２０２５５５０１２３", "+", ""} {
		if _, err := normalizePhone(in); err == nil {
			t.Errorf("accepted malformed international phone %q", in)
		}
	}
}

func TestCodeDeliveryKeepsFallbackAndTimeout(t *testing.T) {
	now := time.Unix(1000, 0)
	code, hash, err := describeSentCode(&tg.AuthSentCode{
		Type: &tg.AuthSentCodeTypeApp{}, PhoneCodeHash: "keep-this-hash", NextType: &tg.AuthCodeTypeSMS{}, Timeout: 60,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !code.sent || code.next != "短信" || !code.retryAt.Equal(now.Add(time.Minute)) || hash != "keep-this-hash" {
		t.Fatalf("lost Telegram delivery/fallback metadata: %#v, hash=%q", code, hash)
	}
	if !strings.Contains(code.notice, "不是短信") {
		t.Errorf("App delivery must not be presented as SMS: %s", code.notice)
	}
}

func TestUnsupportedDeliveryDoesNotClaimCodeWasSent(t *testing.T) {
	for _, typ := range []tg.AuthSentCodeTypeClass{&tg.AuthSentCodeTypeFirebaseSMS{}, &tg.AuthSentCodeTypeSetUpEmailRequired{}} {
		code, _, err := describeSentCode(&tg.AuthSentCode{Type: typ}, time.Unix(1000, 0))
		if err != nil || code.sent || code.notice == "" {
			t.Errorf("%T falsely presented as delivered: %#v, %v", typ, code, err)
		}
	}
	code, _, err := describeSentCode(&tg.AuthSentCodePaymentRequired{PhoneCodeHash: "hash"}, time.Unix(1000, 0))
	if err != nil || code.sent || !strings.Contains(code.notice, "付费") {
		t.Errorf("payment requirement not explained: %#v, %v", code, err)
	}
}

func TestResendRequiresTelegramFallbackAndElapsedTimeout(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   codeState
		status int
	}{
		{name: "must wait", code: codeState{next: "短信", retryAt: time.Now().Add(time.Hour)}, status: http.StatusTooManyRequests},
		{name: "cannot force SMS", code: codeState{}, status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 不启动连接。等待限制必须在任何 Telegram RPC 之前拦截。
			a := &Account{client: &telegram.Client{}, connCtx: context.Background(), step: api.StepCode,
				phone: "12025550123", codeHash: "original-hash", code: tc.code}
			err := a.ResendCode(context.Background())
			var apiErr *Error
			if !errors.As(err, &apiErr) || apiErr.Status != tc.status {
				t.Fatalf("resend returned %v, want HTTP %d", err, tc.status)
			}
			if a.codeHash != "original-hash" || a.step != api.StepCode || a.busy {
				t.Fatal("rejected resend lost the original login request")
			}
		})
	}
}

func TestCanceledQRCompletionCannotLogInNewAttempt(t *testing.T) {
	a := &Account{client: &telegram.Client{}, connGen: 1, loginGen: 2, step: api.StepQR}
	canceled := false
	a.qrCancel = func() { canceled = true }
	a.CancelLogin()
	err := a.authorized(1, 2, &tg.AuthAuthorization{User: &tg.User{ID: 123, FirstName: "Old QR"}})
	if !canceled || !errors.Is(err, context.Canceled) || a.step != api.StepPhone || a.user != nil {
		t.Fatalf("old QR replaced canceled login: step=%s, user=%v, err=%v", a.step, a.user, err)
	}
}
