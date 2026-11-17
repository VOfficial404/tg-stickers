package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/VOfficial404/tg-stickers/api"
)

const version = "0.1.0"

// app 的界面状态只在主线程读写，后台请求通过 post 回到主线程。
type app struct {
	post      func(func())
	store     store
	openFiles func(title string, gifOnly bool) ([]string, error)

	ctx    context.Context
	cancel context.CancelFunc
	// epoch 在断开连接时加一，之前发出的请求回来时发现对不上就丢掉结果
	epoch int

	server, token          string
	remember, connecting   bool
	connectErr             string
	client                 *api.Client
	st                     *api.State
	stateBusy              bool
	page                   string
	width                  float32
	narrow                 bool
	ticks                  int
	toasts                 []string
	confirmOpen            bool
	confirmTitle           string
	confirmText, confirmOK string
	confirmAction          func()

	login loginForm

	sets       []api.SetSummary
	setsLoaded bool
	setsBusy   bool
	setsErr    string
	set        *api.Set // 打开的贴纸包
	setName    string   // 正在打开的贴纸包的链接名
	setBusy    bool
	setErr     string
	selected   int64 // 选中的贴纸
	editEmoji  string
	busy       map[string]bool
	rename     struct {
		open  bool
		title string
	}
	create createForm

	// 上传选项：贴纸对应的表情，动图超过 3 秒时加速还是截取
	emoji string
	fit   int
	queue []*job

	gifs        []api.GIF
	gifsLoaded  bool
	gifsBusy    bool
	gifsErr     string
	selectedGIF int64

	thumbs      map[int64]*ui.Bitmap
	thumbTried  map[int64]bool
	thumbFailed map[int64]bool
	thumbQueue  []thumbReq
	thumbActive int
}

type loginForm struct {
	apiID, apiHash, phone, code, password string
	busy                                  bool
	err                                   string
}

type createForm struct {
	open                   bool
	title, shortName, err  string
	files                  []string
	busy, checking         bool
	shortOK                *bool
	shortReason, checkedAs string
}

// job 是上传队列里的一个文件；target 是贴纸包的链接名，空表示存进 GIF 收藏
type job struct {
	path, target string
	emoji, fit   string
	state        jobState
	err          string
}

type jobState int

const (
	jobWaiting jobState = iota
	jobRunning
	jobDone
	jobFailed
)

type thumbReq struct {
	id  int64
	gif bool
}

var fits = []string{api.FitSpeed, api.FitTrim}

func newApp(s store, post func(func())) *app {
	p := s.load()
	ctx, cancel := context.WithCancel(context.Background())
	a := &app{post: post, store: s, ctx: ctx, cancel: cancel, server: p.Server, remember: p.Remember, page: pageSets,
		busy: map[string]bool{}, thumbs: map[int64]*ui.Bitmap{}, thumbTried: map[int64]bool{}, thumbFailed: map[int64]bool{}}
	if i := slices.Index(fits, p.Fit); i >= 0 {
		a.fit = i
	}
	return a
}

func (a *app) async(work func() func()) {
	post := a.post
	go func() {
		if apply := work(); apply != nil {
			post(apply)
		}
	}()
}

// request 在后台调用接口，结果回到主线程；断开连接后回来的结果丢掉
func request[T any](a *app, call func(context.Context, *api.Client) (T, error), done func(T, error)) {
	cl, ctx, epoch := a.client, a.ctx, a.epoch
	if cl == nil {
		return
	}
	a.async(func() func() {
		v, err := call(ctx, cl)
		return func() {
			if epoch == a.epoch {
				done(v, err)
			}
		}
	})
}

func (a *app) toast(s string) { a.toasts = append(a.toasts, s) }

// failed 处理接口的错误：令牌不对就断开，Telegram 登录失效就刷新状态回到登录页；返回给界面显示的说明
func (a *app) failed(err error) string {
	if api.IsUnauthorized(err) {
		a.disconnect()
		a.connectErr = "令牌不对，请检查服务器令牌"
		return a.connectErr
	}
	var e *api.Error
	if errors.As(err, &e) && (e.Status == http.StatusConflict || e.Status == http.StatusServiceUnavailable) {
		a.refreshState()
	}
	return err.Error()
}

func (a *app) savePrefs() error {
	if err := a.store.save(prefs{Server: a.server, Remember: a.remember, Fit: fits[a.fit]}); err != nil {
		return err
	}
	token := ""
	if a.remember {
		token = a.token
	}
	return a.store.setToken(token)
}

func (a *app) autoConnect() {
	if !a.remember {
		return
	}
	if t, err := a.store.token(); err == nil && t != "" {
		a.token = t
		a.connect()
	}
}

func (a *app) connect() {
	if a.connecting {
		return
	}
	base, err := api.NormalizeBase(a.server)
	if err != nil {
		a.connectErr = err.Error()
		return
	}
	a.server, a.connecting, a.connectErr = base, true, ""
	cl, ctx, epoch := api.NewClient(base, a.token), a.ctx, a.epoch
	a.async(func() func() {
		_, err := cl.Ping(ctx)
		var st api.State
		if err == nil {
			st, err = cl.State(ctx)
		}
		return func() {
			if epoch != a.epoch {
				return
			}
			a.connecting = false
			if err != nil {
				if api.IsUnauthorized(err) {
					a.connectErr = "令牌不对，请检查服务器令牌"
				} else {
					a.connectErr = "连接失败：" + err.Error()
				}
				return
			}
			a.client = cl
			if err := a.savePrefs(); err != nil {
				a.toast("连接成功，但保存连接信息失败：" + err.Error())
			}
			a.applyState(st)
		}
	})
}

func (a *app) disconnect() {
	a.epoch++
	a.cancel()
	a.ctx, a.cancel = context.WithCancel(context.Background())
	*a = app{
		post: a.post, store: a.store, openFiles: a.openFiles,
		ctx: a.ctx, cancel: a.cancel, epoch: a.epoch,
		server: a.server, token: a.token, remember: a.remember, page: pageSets,
		emoji: a.emoji, fit: a.fit, width: a.width, narrow: a.narrow, ticks: a.ticks,
		busy: map[string]bool{}, thumbs: map[int64]*ui.Bitmap{}, thumbTried: map[int64]bool{}, thumbFailed: map[int64]bool{},
	}
}

func (a *app) applyState(st api.State) {
	wasReady := a.st != nil && a.st.Step == api.StepReady
	a.st = &st
	if st.Step == api.StepReady && !wasReady {
		a.login = loginForm{}
		a.navigate(a.page)
	}
	if st.Step != api.StepReady && wasReady {
		a.sets, a.setsLoaded, a.set, a.gifs, a.gifsLoaded = nil, false, nil, nil, false
	}
}

func (a *app) refreshState() {
	if a.stateBusy {
		return
	}
	a.stateBusy = true
	request(a, func(ctx context.Context, c *api.Client) (api.State, error) { return c.State(ctx) }, func(st api.State, err error) {
		a.stateBusy = false
		if err != nil {
			if api.IsUnauthorized(err) {
				a.failed(err)
			}
			return
		}
		a.applyState(st)
	})
}

func (a *app) tick() {
	a.ticks++
	if a.client == nil || a.st == nil {
		return
	}
	// 登录过程中每 2 秒看一次状态，登录后每分钟看一次，发现在别处被注销
	if (a.st.Step != api.StepReady && a.ticks%2 == 0) || a.ticks%60 == 0 {
		a.refreshState()
	}
}

func (a *app) navigate(page string) {
	a.page = page
	switch page {
	case pageSets:
		if !a.setsLoaded {
			a.loadSets()
		}
	case pageGIFs:
		if !a.gifsLoaded {
			a.loadGIFs()
		}
	}
}

/* 登录 */

// loginStep 提交登录的一步
func (a *app) loginStep(call func(context.Context, *api.Client) (api.State, error)) {
	if a.login.busy {
		return
	}
	a.login.busy, a.login.err = true, ""
	request(a, call, func(st api.State, err error) {
		a.login.busy = false
		if err != nil {
			a.login.err = a.failed(err)
			return
		}
		a.login.code, a.login.password = "", ""
		a.applyState(st)
	})
}

func (a *app) logout() {
	a.ask("退出 Telegram 登录", "服务端会在 Telegram 上注销这次登录并删掉保存的会话。贴纸包不受影响，以后可以重新登录。", "退出登录", func() {
		a.loginStep(func(ctx context.Context, c *api.Client) (api.State, error) { return c.Logout(ctx) })
	})
}

func (a *app) ask(title, text, ok string, action func()) {
	a.confirmOpen, a.confirmTitle, a.confirmText, a.confirmOK, a.confirmAction = true, title, text, ok, action
}

/* 贴纸包 */

func (a *app) loadSets() {
	if a.setsBusy {
		return
	}
	a.setsBusy, a.setsErr = true, ""
	request(a, func(ctx context.Context, c *api.Client) ([]api.SetSummary, error) { return c.Sets(ctx) }, func(s []api.SetSummary, err error) {
		a.setsBusy = false
		if err != nil {
			a.setsErr = a.failed(err)
			return
		}
		a.sets, a.setsLoaded = s, true
	})
}

func (a *app) openSet(name string) {
	a.setName, a.setBusy, a.setErr, a.selected = name, true, "", 0
	if a.set != nil && a.set.ShortName != name {
		a.set = nil
	}
	request(a, func(ctx context.Context, c *api.Client) (api.Set, error) { return c.Set(ctx, name) }, func(s api.Set, err error) {
		if a.setName != name {
			return
		}
		a.setBusy = false
		if err != nil {
			a.setErr = a.failed(err)
			return
		}
		a.applySet(s)
	})
}

func (a *app) closeSet() { a.set, a.setName, a.setErr, a.selected = nil, "", "", 0 }

// applySet 用接口返回的贴纸包更新打开的那个和列表里的摘要
func (a *app) applySet(s api.Set) {
	if a.setName == s.ShortName {
		a.set = &s
		if !slices.ContainsFunc(s.Stickers, func(st api.Sticker) bool { return st.ID == a.selected }) {
			a.selected = 0
		}
	}
	i := slices.IndexFunc(a.sets, func(x api.SetSummary) bool { return x.ShortName == s.ShortName })
	if i >= 0 {
		a.sets[i] = s.SetSummary
	} else if a.setsLoaded {
		a.sets = append([]api.SetSummary{s.SetSummary}, a.sets...)
	}
}

// setAction 对打开的贴纸包做一个修改，key 防止重复点击
func (a *app) setAction(key, success string, call func(context.Context, *api.Client) (api.Set, error)) {
	if a.busy[key] {
		return
	}
	a.busy[key] = true
	request(a, call, func(s api.Set, err error) {
		a.busy[key] = false
		if err != nil {
			a.toast(a.failed(err))
			return
		}
		a.applySet(s)
		if success != "" {
			a.toast(success)
		}
	})
}

func (a *app) moveSticker(id int64, pos int) {
	a.setAction("sticker", "", func(ctx context.Context, c *api.Client) (api.Set, error) {
		return c.PatchSticker(ctx, id, api.StickerPatch{Position: &pos})
	})
}

func (a *app) saveEmoji(id int64, emoji string) {
	a.setAction("sticker", "表情已修改", func(ctx context.Context, c *api.Client) (api.Set, error) {
		return c.PatchSticker(ctx, id, api.StickerPatch{Emoji: emoji})
	})
}

func (a *app) deleteSticker(id int64) {
	last := a.set != nil && len(a.set.Stickers) == 1
	text := "这张贴纸会从贴纸包里删掉，已经发出去的消息里仍然看得到。"
	if last {
		text = "这是贴纸包里最后一张贴纸，删掉后 Telegram 可能连贴纸包一起删除。"
	}
	a.ask("删除贴纸", text, "删除", func() {
		a.setAction("sticker", "已删除", func(ctx context.Context, c *api.Client) (api.Set, error) { return c.DeleteSticker(ctx, id) })
	})
}

func (a *app) renameSet() {
	name, title := a.set.ShortName, a.rename.title
	a.rename.open = false
	a.setAction("rename", "已改名", func(ctx context.Context, c *api.Client) (api.Set, error) {
		return c.PatchSet(ctx, name, api.SetPatch{Title: title})
	})
}

func (a *app) deleteSet() {
	name, title := a.set.ShortName, a.set.Title
	a.ask("删除贴纸包", "「"+title+"」会从 Telegram 上彻底删除，所有人都不能再添加，这不能撤销。", "删除贴纸包", func() {
		a.busy["delete-set"] = true
		request(a, func(ctx context.Context, c *api.Client) (struct{}, error) { return struct{}{}, c.DeleteSet(ctx, name) }, func(_ struct{}, err error) {
			a.busy["delete-set"] = false
			if err != nil {
				a.toast(a.failed(err))
				return
			}
			a.sets = slices.DeleteFunc(a.sets, func(s api.SetSummary) bool { return s.ShortName == name })
			if a.setName == name {
				a.closeSet()
			}
			a.toast("已删除「" + title + "」")
		})
	})
}

/* 新建贴纸包 */

func (a *app) startCreate() {
	a.create = createForm{open: true}
}

func (a *app) checkShortName() {
	name := a.create.shortName
	if name == "" || a.create.checking || a.create.checkedAs == name {
		return
	}
	a.create.checking = true
	request(a, func(ctx context.Context, c *api.Client) (api.ShortNameCheck, error) {
		return c.CheckShortName(ctx, name)
	}, func(r api.ShortNameCheck, err error) {
		a.create.checking = false
		if err != nil {
			a.create.err = a.failed(err)
			return
		}
		a.create.checkedAs, a.create.shortOK, a.create.shortReason = name, &r.Available, r.Reason
	})
}

func (a *app) pickCreateFiles() {
	pick := a.openFiles
	a.async(func() func() {
		files, err := pick("选择贴纸", false)
		return func() {
			if err != nil {
				a.create.err = "打开文件失败：" + err.Error()
				return
			}
			a.create.files = append(a.create.files, a.checkFiles(files)...)
		}
	})
}

// submitCreate 用第一个文件新建贴纸包，其余的排进上传队列
func (a *app) submitCreate() {
	f := &a.create
	if f.busy {
		return
	}
	if f.title == "" || f.shortName == "" {
		f.err = "请填写名字和链接名"
		return
	}
	if len(f.files) == 0 {
		f.err = "至少选一张图片或动图作为第一张贴纸"
		return
	}
	f.busy, f.err = true, ""
	cs := api.CreateSet{Title: f.title, ShortName: f.shortName, Upload: api.Upload{Emoji: a.emoji, Fit: fits[a.fit]}}
	first, rest := f.files[0], f.files[1:]
	request(a, func(ctx context.Context, c *api.Client) (api.Set, error) { return c.CreateSet(ctx, cs, first) }, func(s api.Set, err error) {
		a.create.busy = false
		if err != nil {
			a.create.err = a.failed(err)
			return
		}
		a.create.open = false
		a.setName = s.ShortName
		a.applySet(s)
		a.page = pageSets
		a.toast("已新建「" + s.Title + "」")
		a.enqueue(rest, s.ShortName)
	})
}

/* 上传队列 */

// checkFiles 去掉超过大小上限、读不到的文件，并提示
func (a *app) checkFiles(paths []string) []string {
	limit := int64(50 << 20)
	if a.st != nil && a.st.Limits.MaxUploadBytes > 0 {
		limit = a.st.Limits.MaxUploadBytes
	}
	var ok []string
	for _, p := range paths {
		fi, err := os.Stat(p)
		switch {
		case err != nil:
			a.toast("读不到 " + filepath.Base(p) + "：" + err.Error())
		case fi.IsDir():
			a.toast(filepath.Base(p) + " 是文件夹，请选文件")
		case fi.Size() > limit:
			a.toast(fmt.Sprintf("%s 超过 %d MB，跳过", filepath.Base(p), limit>>20))
		default:
			ok = append(ok, p)
		}
	}
	return ok
}

// enqueue 把文件排进上传队列；target 为空表示存进 GIF 收藏
func (a *app) enqueue(paths []string, target string) {
	for _, p := range a.checkFiles(paths) {
		a.queue = append(a.queue, &job{path: p, target: target, emoji: a.emoji, fit: fits[a.fit]})
	}
	a.pump()
}

func (a *app) pickFiles(target string) {
	pick := a.openFiles
	title := "选择要加进贴纸包的图片或动图"
	if target == "" {
		title = "选择要存进 GIF 收藏的动图"
	}
	a.async(func() func() {
		files, err := pick(title, target == "")
		return func() {
			if err != nil {
				a.toast("打开文件失败：" + err.Error())
				return
			}
			a.enqueue(files, target)
		}
	})
}

// pump 一次传一个：贴纸要按顺序进贴纸包，ffmpeg 也吃 CPU
func (a *app) pump() {
	var next *job
	for _, j := range a.queue {
		switch j.state {
		case jobRunning:
			return
		case jobWaiting:
			if next == nil {
				next = j
			}
		}
	}
	if next == nil {
		return
	}
	j := next
	j.state = jobRunning
	if j.target == "" {
		request(a, func(ctx context.Context, c *api.Client) ([]api.GIF, error) { return c.AddGIF(ctx, j.path) }, func(g []api.GIF, err error) {
			if a.finish(j, err) {
				a.gifs, a.gifsLoaded = g, true
			}
		})
		return
	}
	u := api.Upload{Emoji: j.emoji, Fit: j.fit}
	request(a, func(ctx context.Context, c *api.Client) (api.Set, error) {
		return c.AddSticker(ctx, j.target, u, j.path)
	}, func(s api.Set, err error) {
		if a.finish(j, err) {
			a.applySet(s)
		}
	})
}

func (a *app) finish(j *job, err error) bool {
	if err != nil {
		j.state, j.err = jobFailed, a.failed(err)
	} else {
		j.state = jobDone
	}
	a.pump()
	return err == nil
}

func (a *app) retry(j *job) {
	j.state, j.err = jobWaiting, ""
	a.pump()
}

// clearFinished 清掉队列里已经完成的
func (a *app) clearFinished() {
	a.queue = slices.DeleteFunc(a.queue, func(j *job) bool { return j.state == jobDone })
}

/* GIF 收藏 */

func (a *app) loadGIFs() {
	if a.gifsBusy {
		return
	}
	a.gifsBusy, a.gifsErr = true, ""
	request(a, func(ctx context.Context, c *api.Client) ([]api.GIF, error) { return c.GIFs(ctx) }, func(g []api.GIF, err error) {
		a.gifsBusy = false
		if err != nil {
			a.gifsErr = a.failed(err)
			return
		}
		a.gifs, a.gifsLoaded = g, true
	})
}

func (a *app) deleteGIF(id int64) {
	if a.busy["gif"] {
		return
	}
	a.busy["gif"] = true
	request(a, func(ctx context.Context, c *api.Client) ([]api.GIF, error) { return c.DeleteGIF(ctx, id) }, func(g []api.GIF, err error) {
		a.busy["gif"] = false
		if err != nil {
			a.toast(a.failed(err))
			return
		}
		a.gifs, a.selectedGIF = g, 0
		a.toast("已从 GIF 收藏移除")
	})
}

/* 缩略图：最多同时取 4 张 */

func (a *app) thumb(id int64, gif bool) *ui.Bitmap {
	if bm := a.thumbs[id]; bm != nil || id == 0 || a.thumbTried[id] {
		return bm
	}
	a.thumbTried[id] = true
	a.thumbQueue = append(a.thumbQueue, thumbReq{id: id, gif: gif})
	a.pumpThumbs()
	return nil
}

func (a *app) pumpThumbs() {
	for a.thumbActive < 4 && len(a.thumbQueue) > 0 {
		r := a.thumbQueue[0]
		a.thumbQueue = a.thumbQueue[1:]
		a.thumbActive++
		request(a, func(ctx context.Context, c *api.Client) ([]byte, error) {
			if r.gif {
				return c.GIFThumb(ctx, r.id)
			}
			return c.StickerThumb(ctx, r.id)
		}, func(b []byte, err error) {
			a.thumbActive--
			var bm *ui.Bitmap
			if err == nil {
				bm, err = ui.DecodeBitmap(b)
			}
			if err != nil {
				a.thumbFailed[r.id] = true
			} else {
				a.thumbs[r.id] = bm
			}
			a.pumpThumbs()
		})
	}
}
