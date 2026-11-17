package main

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/VOfficial404/tg-stickers/api"
)

/* 外框 */

func (a *app) view(c *ui.Context) {
	applyTheme(c)
	for _, s := range a.toasts {
		c.Toast(s)
	}
	a.toasts = nil
	a.width, _ = c.Size()
	a.narrow = a.width < 760
	switch {
	case a.client == nil || a.st == nil:
		a.connectView(c)
	case a.st.Step != api.StepReady:
		a.loginView(c)
	default:
		a.shell(c)
	}
	a.dialogs(c)
}

func (a *app) shell(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		if !a.narrow {
			a.sidebar(c)
		}
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Scroll(c.Key("page-" + a.page)).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Padding(28, 32, 32).Gap(20).MaxWidth(1100).Children(func() {
					switch a.page {
					case pageSets:
						if a.setName != "" {
							a.setView(c)
						} else {
							a.setsView(c)
						}
					case pageGIFs:
						a.gifsView(c)
					case pageAccount:
						a.accountView(c)
					}
				})
			})
			a.queueBar(c)
			if a.narrow {
				a.tabBar(c)
			}
		})
	})
}

// pageHeader 是页面顶部：标题、一句说明，右边是这一页的操作
func (a *app) pageHeader(c *ui.Context, title, desc string, actions func()) {
	ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
		ui.Column(c).Gap(4).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, title).FontSize(26).Bold()
			if desc != "" {
				muted(c, desc).FontSize(13)
			}
		})
		if actions != nil {
			ui.Row(c).Gap(8).Children(actions)
		}
	})
}

// iconButton 是带图标的按钮
func iconButton(c *ui.Context, key, label string, ic *ui.SVG, primary bool) ui.Element {
	var b ui.Element
	if primary {
		b = ui.PrimaryButton(c.Key(key), "")
	} else {
		b = ui.Button(c.Key(key), "")
	}
	return b.Label(label).Children(func() {
		ui.Icon(c, ic)
		ui.Text(c, label).SingleLine()
	})
}

func (a *app) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Width(216).Padding(20, 14, 16).Gap(20).Background(t.Surface).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Gap(10).Padding(0, 6).Children(func() {
			brandMark(c, 26)
			ui.Text(c, "TG Stickers").FontSize(17).Bold()
		})
		page := a.page
		ui.Sidebar(c, &page, func() {
			for _, p := range navPages {
				item := ui.SidebarItem(c, p.id, p.icon, p.id)
				if p.id == pageSets && len(a.sets) > 0 {
					item.Children(func() { muted(c, strconv.Itoa(len(a.sets))).FontSize(12) })
				}
				if item.Clicked() {
					a.navigate(p.id)
				}
			}
		}).Grow(1).Background(t.Surface)
		a.connectionInfo(c)
	})
}

// tabBar 是窄窗口底部的标签栏：图标在上，文字在下
func (a *app) tabBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(4, 6, 6).Background(t.Surface).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		for _, p := range navPages {
			col := t.TextMuted
			if a.page == p.id {
				col = t.Accent
			}
			ui.ButtonBase(c.Key("tab-"+p.id)).Grow(1).Basis(0).Gap(2).Padding(6, 0).Center().Label(p.id).
				OnClick(func() { a.navigate(p.id) }).Children(func() {
				ui.Icon(c, p.icon).FontSize(20).TextColor(col)
				ui.Text(c, p.id).FontSize(11).TextColor(col).SingleLine()
			})
		}
	})
}

// connectionInfo 是侧边栏底部：登录的是谁、连的是哪台服务器
func (a *app) connectionInfo(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(6).Padding(12).Radius(10).Background(t.Background).Children(func() {
		if u := a.st.User; u != nil {
			ui.Row(c).Gap(8).Children(func() {
				ui.Box(c).Size(8, 8).Radius(4).Background(t.Success)
				ui.Text(c, u.Name).FontSize(12).FontWeight(600).SingleLine().Ellipsis("…")
			})
		}
		where := a.client.Base()
		muted(c, where).FontSize(11).Selectable().SingleLine().Ellipsis("…").Tooltip(where)
		muted(c, "服务端 "+a.st.Version+" · 客户端 "+version).FontSize(11)
	})
}

/* 连接页 */

func (a *app) connectView(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Fill().Background(t.Background).Children(func() {
		ui.Column(c).FillWidth().MinHeight(560).Padding(48, 24).Center().Children(func() {
			ui.Column(c).Width(min(440, a.width-48)).Gap(22).Children(func() {
				ui.Column(c).Gap(10).AlignItems(ui.Center).Children(func() {
					brandMark(c, 52)
					ui.Text(c, "TG Stickers").FontSize(28).Bold()
					muted(c, "用你自己的 Telegram 账号管理贴纸包，把 GIF 一键变成视频贴纸。").TextAlign(ui.Center)
				})
				panel(c).Padding(24).Gap(18).Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.18)).Children(func() {
					ui.Field(c, "服务器地址", func() {
						ui.TextInput(c.Key("server"), &a.server).Placeholder("http://127.0.0.1:8787").Disabled(a.connecting)
					})
					ui.Field(c, "令牌", func() {
						ui.TextInput(c.Key("token"), &a.token).Password().Label("令牌").Disabled(a.connecting).OnSubmit(a.connect)
					})
					if ui.Checkbox(c, &a.remember, "记住令牌，下次打开自动连接").Disabled(a.connecting).Changed() && !a.remember {
						// 取消勾选时马上删掉记住的令牌，不等下次连接成功
						if err := a.savePrefs(); err != nil {
							a.connectErr = "删除记住的令牌失败：" + err.Error()
						}
					}
					muted(c, "令牌在服务器上用 tg-stickers token 查看（Docker 里是 docker compose exec tg-stickers tg-stickers token），记住后保存在系统凭据管理器里。").FontSize(12)
					errorText(c, a.connectErr)
					ui.PrimaryButton(c.Key("connect"), "连接").Disabled(a.connecting).OnClick(a.connect)
					if a.connecting {
						ui.Row(c).Gap(8).Center().Children(func() {
							ui.Spinner(c)
							muted(c, "正在连接…").FontSize(13)
						})
					}
				})
			})
		})
	})
}

/* 登录页：api_id → 手机号 → 验证码 → 两步验证密码 */

func (a *app) loginView(c *ui.Context) {
	t := c.Theme()
	st, f := a.st, &a.login
	ui.Scroll(c).Fill().Background(t.Background).Children(func() {
		ui.Column(c).FillWidth().MinHeight(560).Padding(48, 24).Center().Children(func() {
			ui.Column(c).Width(min(460, a.width-48)).Gap(22).Children(func() {
				ui.Column(c).Gap(10).AlignItems(ui.Center).Children(func() {
					brandMark(c, 44)
					ui.Text(c, "登录 Telegram").FontSize(24).Bold()
					muted(c, "登录的是你自己的账号，会话只保存在你的服务器上。").TextAlign(ui.Center).FontSize(13)
				})
				panel(c).Padding(24).Gap(16).Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.18)).Children(func() {
					switch st.Step {
					case api.StepCredentials:
						a.credentialsForm(c)
					case api.StepConnecting:
						ui.Row(c).Gap(10).Children(func() {
							ui.Spinner(c)
							ui.Text(c, "正在连接 Telegram…")
						})
						if st.Error != "" {
							errorText(c, st.Error)
							muted(c, "服务器连不上 Telegram 时，可以用环境变量 TG_STICKERS_PROXY 设置 SOCKS5 代理。").FontSize(12)
						}
					case api.StepPhone:
						ui.Field(c, "手机号（带国家码）", func() {
							ui.TextInput(c.Key("phone"), &f.phone).Placeholder("+86 138 0000 0000").Disabled(f.busy).OnSubmit(a.sendCode)
						})
						ui.PrimaryButton(c.Key("send-code"), "发送验证码").Disabled(f.busy).OnClick(a.sendCode)
					case api.StepCode:
						muted(c, "验证码已发到"+st.CodeVia+"。").FontSize(13)
						ui.Field(c, "验证码", func() {
							ui.TextInput(c.Key("code"), &f.code).Disabled(f.busy).AutoFocus().OnSubmit(a.signIn)
						})
						ui.Row(c).Gap(8).Children(func() {
							ui.PrimaryButton(c.Key("sign-in"), "登录").Disabled(f.busy).OnClick(a.signIn)
							ui.Button(c.Key("resend"), "重新发送").Disabled(f.busy).OnClick(a.sendCode)
							ui.Button(c.Key("change-phone"), "换个手机号").Disabled(f.busy).OnClick(a.cancelLogin)
						})
					case api.StepPassword:
						muted(c, "这个账号开了两步验证，请输入密码。").FontSize(13)
						ui.Field(c, "两步验证密码", func() {
							ui.TextInput(c.Key("password"), &f.password).Password().Disabled(f.busy).AutoFocus().OnSubmit(a.submitPassword)
						})
						if st.PasswordHint != "" {
							muted(c, "密码提示："+st.PasswordHint).FontSize(12)
						}
						ui.Row(c).Gap(8).Children(func() {
							ui.PrimaryButton(c.Key("password-ok"), "登录").Disabled(f.busy).OnClick(a.submitPassword)
							ui.Button(c.Key("password-cancel"), "重新开始").Disabled(f.busy).OnClick(a.cancelLogin)
						})
					}
					errorText(c, f.err)
					if f.busy {
						ui.Row(c).Gap(8).Children(func() {
							ui.Spinner(c)
							muted(c, "请稍候…").FontSize(13)
						})
					}
				})
				ui.Row(c).Gap(8).Center().Children(func() {
					muted(c, a.client.Base()).FontSize(12).SingleLine()
					ui.Button(c.Key("disconnect"), "断开服务器").OnClick(a.disconnect)
				})
			})
		})
	})
}

func (a *app) credentialsForm(c *ui.Context) {
	f := &a.login
	muted(c, "第一次使用要一个 Telegram 应用的 api_id 和 api_hash：用要登录的账号打开 my.telegram.org，进入 API development tools，随便填个应用名创建，就能看到这两个值。").FontSize(13)
	ui.Link(c, "打开 my.telegram.org", "https://my.telegram.org/apps")
	ui.Field(c, "api_id", func() {
		ui.TextInput(c.Key("api-id"), &f.apiID).Placeholder("1234567").Disabled(f.busy)
	})
	ui.Field(c, "api_hash", func() {
		ui.TextInput(c.Key("api-hash"), &f.apiHash).Placeholder("32 位字母和数字").Disabled(f.busy).OnSubmit(a.saveCredentials)
	})
	ui.PrimaryButton(c.Key("save-credentials"), "保存").Disabled(f.busy).OnClick(a.saveCredentials)
}

func (a *app) saveCredentials() {
	id, err := strconv.Atoi(strings.TrimSpace(a.login.apiID))
	if err != nil || id <= 0 {
		a.login.err = "api_id 是一串数字"
		return
	}
	cr := api.Credentials{APIID: id, APIHash: strings.TrimSpace(a.login.apiHash)}
	a.loginStep(func(ctx context.Context, c *api.Client) (api.State, error) { return c.SetCredentials(ctx, cr) })
}

func (a *app) sendCode() {
	phone := a.login.phone
	a.loginStep(func(ctx context.Context, c *api.Client) (api.State, error) { return c.SendCode(ctx, phone) })
}

func (a *app) signIn() {
	code := a.login.code
	a.loginStep(func(ctx context.Context, c *api.Client) (api.State, error) { return c.SignIn(ctx, code) })
}

func (a *app) submitPassword() {
	pw := a.login.password
	a.loginStep(func(ctx context.Context, c *api.Client) (api.State, error) { return c.Password(ctx, pw) })
}

func (a *app) cancelLogin() {
	a.loginStep(func(ctx context.Context, c *api.Client) (api.State, error) { return c.CancelLogin(ctx) })
}

/* 账号页 */

func (a *app) accountView(c *ui.Context) {
	a.pageHeader(c, pageAccount, "", nil)
	u := a.st.User
	panel(c).Children(func() {
		sectionTitle(c, "Telegram 账号")
		if u != nil {
			ui.Text(c, u.Name).FontSize(18).Bold().Selectable()
			var meta []string
			if u.Username != "" {
				meta = append(meta, "@"+u.Username)
			}
			if u.Phone != "" {
				meta = append(meta, "+"+u.Phone)
			}
			meta = append(meta, "ID "+strconv.FormatInt(u.ID, 10))
			muted(c, strings.Join(meta, " · ")).Selectable()
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c.Key("logout"), "退出 Telegram 登录").Disabled(a.login.busy).OnClick(a.logout)
		})
		errorText(c, a.login.err)
	})
	panel(c).Children(func() {
		sectionTitle(c, "服务器")
		muted(c, a.client.Base()).Selectable()
		ff := "ffmpeg " + a.st.FFmpeg
		if a.st.FFmpeg == "" {
			ff = "没有找到 ffmpeg，没法转换贴纸和 GIF。Docker 镜像里自带；直接运行时请先安装 ffmpeg。"
		}
		muted(c, "服务端 "+a.st.Version+" · "+ff).FontSize(13)
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c.Key("disconnect"), "断开服务器").OnClick(a.disconnect)
		})
	})
}

/* 对话框 */

func (a *app) dialogs(c *ui.Context) {
	if ui.AlertDialog(c, &a.confirmOpen, a.confirmTitle, a.confirmText, "取消", a.confirmOK) == 1 && a.confirmAction != nil {
		a.confirmAction()
	}
	a.createDialog(c)
	a.renameDialog(c)
}
