package main

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

func (a *app) gifsView(c *ui.Context) {
	t := c.Theme()
	a.pageHeader(c, pageGIFs, "Telegram「GIF」标签页的收藏。超过 3 秒也保留完整时长，云端转换为无声 MP4。", func() {
		iconButton(c, "refresh-gifs", "刷新", iconRefresh, false).Disabled(a.gifsBusy).OnClick(a.loadGIFs)
		if a.selectedGIF != 0 {
			id := a.selectedGIF
			iconButton(c, "delete-gif", "从收藏移除", iconTrash, false).Disabled(a.busy["gif"]).OnClick(func() { a.deleteGIF(id) })
		}
		iconButton(c, "add-gif", "添加 GIF", iconUpload, true).OnClick(func() { a.pickFiles("") })
	})
	zone := ui.Column(c).Gap(14)
	if files := zone.DroppedFiles(); files != nil {
		a.enqueue(files, "")
	}
	zone.Children(func() {
		drop := panel(c).Padding(16).Gap(12)
		if zone.FileDragOver() {
			drop.Border(2, t.Accent).Background(t.Accent.Alpha(0.06))
		}
		drop.Children(func() {
			muted(c, "把动图拖到这一页的任何地方就能加进收藏。点一个选中，再点「从收藏移除」。").FontSize(13)
			a.mediaFields(c, &a.gifMedia, true, false)
		})
		errorText(c, a.gifsErr)
		if a.gifsBusy && !a.gifsLoaded {
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				muted(c, "正在读取 GIF 收藏…")
			})
			return
		}
		if a.gifsLoaded && len(a.gifs) == 0 {
			muted(c, "收藏里还没有 GIF")
			return
		}
		ui.Grid(c).Columns(a.gridColumns(172)).Gap(10).Children(func() {
			for _, g := range a.gifs {
				sel := g.ID == a.selectedGIF
				bg, border := t.Surface, t.Border
				if sel {
					bg, border = t.Accent.Alpha(0.12), t.Accent
				}
				ui.ButtonBase(c.Key("gif-"+strconv.FormatInt(g.ID, 10))).Padding(8).Gap(6).Radius(12).Background(bg).Border(1, border).
					AlignItems(ui.Center).Label("GIF").OnClick(func() {
					if a.selectedGIF == g.ID {
						a.selectedGIF = 0
					} else {
						a.selectedGIF = g.ID
					}
				}).ContextMenu(func(m *ui.Menu) {
					if m.Item("从收藏移除").Danger().Disabled(a.busy["gif"]).Chosen() {
						a.deleteGIF(g.ID)
					}
				}).Children(func() {
					a.thumbBox(c, g.ID, true, 140)
					if g.W > 0 {
						muted(c, strconv.Itoa(g.W)+"×"+strconv.Itoa(g.H)).FontSize(11)
					}
				})
			}
		})
	})
}
