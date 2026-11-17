package main

import "github.com/egoist/mygo/ui"

// Telegram 蓝只用在主要操作和选中项上；其余颜色跟随系统的深浅色主题。
var (
	brandBlue     = ui.Hex("#2aabee")
	brandBlueDeep = ui.Hex("#1785c8") // 浅色主题里白字放在它上面才够清楚
)

// applyTheme 每帧在系统主题上换上本应用的颜色。MyGo 每帧从系统主题开始，所以这里不会叠加。
func applyTheme(c *ui.Context) {
	t := *c.Theme()
	if t.Dark {
		t = *t.WithAccent(brandBlue)
		t.AccentText = ui.Hex("#06202e")
		t.Background = ui.Hex("#121519")
		t.Surface = ui.Hex("#1a1e24")
		t.SurfaceHover = ui.Hex("#22272e")
		t.SurfacePressed = ui.Hex("#2a3038")
		t.Border = ui.Hex("#2a2f37")
		t.TextMuted = ui.Hex("#929aa6")
	} else {
		t = *t.WithAccent(brandBlueDeep)
		t.Background = ui.Hex("#f4f6f8")
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#eef1f4")
		t.SurfacePressed = ui.Hex("#e3e7ec")
		t.Border = ui.Hex("#dfe3e8")
		t.TextMuted = ui.Hex("#67707c")
	}
	t.Radius = 8
	c.SetTheme(&t)
}

// panel 是内容卡片：比窗口背景亮一级，细边框
func panel(c *ui.Context) ui.Element {
	t := c.Theme()
	return ui.Column(c).Padding(20).Gap(14).Radius(14).Background(t.Surface).Border(1, t.Border)
}

func muted(c *ui.Context, s string) ui.Element { return ui.Text(c, s).TextColor(c.Theme().TextMuted) }

func errorText(c *ui.Context, s string) {
	if s != "" {
		ui.Text(c, s).TextColor(c.Theme().Danger).Selectable()
	}
}

// sectionTitle 是卡片里一组内容的小标题
func sectionTitle(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(12).FontWeight(600).TextColor(c.Theme().TextMuted)
}

// pill 是带底色的小标签
func pill(c *ui.Context, text string, col ui.Color) ui.Element {
	return ui.Box(c).Padding(2, 8).Radius(999).Background(col.Alpha(0.14)).Children(func() {
		ui.Text(c, text).FontSize(11).FontWeight(600).TextColor(col).SingleLine()
	})
}

/* 图标：线条风格，24×24 */

func icon(d string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">` + d + `</svg>`))
}

var (
	iconSets    = icon(`<path d="M14.5 3H6a3 3 0 0 0-3 3v12a3 3 0 0 0 3 3h8.5L21 14.5V6a3 3 0 0 0-3-3h-3.5"/><path d="M21 14h-4a3 3 0 0 0-3 3v4"/><path d="M8.5 10h.01M14.5 10h.01M8.5 14.5c1 1 2.2 1.5 3.5 1.5"/>`)
	iconGIF     = icon(`<rect x="3" y="5" width="18" height="14" rx="3"/><path d="M10 10H8.5A1.5 1.5 0 0 0 7 11.5v1A1.5 1.5 0 0 0 8.5 14H10v-2M13 10v4M16 14v-4h2M16 12h1.5"/>`)
	iconAccount = icon(`<circle cx="12" cy="8.5" r="3.5"/><path d="M5 20c.9-3.8 3.7-5.5 7-5.5s6.1 1.7 7 5.5"/>`)
	iconPlus    = icon(`<path d="M12 5v14M5 12h14"/>`)
	iconUpload  = icon(`<path d="M12 16V4M7 9l5-5 5 5"/><path d="M4 16v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/>`)
	iconBack    = icon(`<path d="m15 6-6 6 6 6"/>`)
	iconRefresh = icon(`<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 4v7h-7"/>`)
	iconLeft    = icon(`<path d="m14 7-5 5 5 5"/>`)
	iconRight   = icon(`<path d="m10 7 5 5-5 5"/>`)
	iconTrash   = icon(`<path d="M4 7h16M10 11v6M14 11v6M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M9 7V4h6v3"/>`)
)

var navPages = []struct {
	id   string
	icon *ui.SVG
}{
	{pageSets, iconSets}, {pageGIFs, iconGIF}, {pageAccount, iconAccount},
}

const (
	pageSets    = "贴纸包"
	pageGIFs    = "GIF 收藏"
	pageAccount = "账号"
)

// brandMark 是界面里的小标记：一个蓝色圆角方块里卷起一角的贴纸
func brandMark(c *ui.Context, size float32) ui.Element {
	return ui.Box(c).Size(size, size).Draw(func(p *ui.Painter, r ui.Rect) {
		s := r.W / 24
		p.Fill(ui.Rect{X: r.X + 1*s, Y: r.Y + 1*s, W: 22 * s, H: 22 * s}, brandBlue, 6*s)
		page := &ui.Path{}
		page.MoveTo(r.X+6*s, r.Y+6*s)
		page.LineTo(r.X+18*s, r.Y+6*s)
		page.LineTo(r.X+18*s, r.Y+13.5*s)
		page.LineTo(r.X+13.5*s, r.Y+18*s)
		page.LineTo(r.X+6*s, r.Y+18*s)
		page.Close()
		p.FillPath(page, ui.Hex("#ffffff"))
		fold := &ui.Path{}
		fold.MoveTo(r.X+18*s, r.Y+13.5*s)
		fold.LineTo(r.X+13.5*s, r.Y+13.5*s)
		fold.LineTo(r.X+13.5*s, r.Y+18*s)
		fold.Close()
		p.FillPath(fold, ui.Hex("#bfe6fa"))
	})
}
