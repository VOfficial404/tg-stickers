package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/VOfficial404/tg-stickers/api"
)

// gridColumns 按窗口宽度算网格的列数
func (a *app) gridColumns(cell float32) int {
	w := a.width - 64
	if !a.narrow {
		w -= 216
	}
	return max(2, int(w/cell))
}

// thumbBox 画一张缩略图，没取到时显示占位
func (a *app) thumbBox(c *ui.Context, id int64, gif bool, size float32) {
	t := c.Theme()
	ui.Box(c).Size(size, size).Center().Children(func() {
		if bm := a.thumb(id, gif); bm != nil {
			ui.Image(c, bm).Size(size, size).Fit(ui.Contain)
		} else if a.thumbFailed[id] {
			ui.Text(c, "无预览").FontSize(11).TextColor(t.TextMuted)
		} else {
			ui.Box(c).Size(size*0.7, size*0.7).Radius(12).Background(t.Border.Alpha(0.5))
		}
	})
}

/* 贴纸包列表 */

func (a *app) setsView(c *ui.Context) {
	t := c.Theme()
	a.pageHeader(c, pageSets, "你的账号创建的贴纸包。点开一个往里加贴纸，或者新建一个。", func() {
		iconButton(c, "refresh-sets", "刷新", iconRefresh, false).Disabled(a.setsBusy).OnClick(a.loadSets)
		iconButton(c, "new-set", "新建贴纸包", iconPlus, true).OnClick(a.startCreate)
	})
	if a.st.FFmpeg == "" {
		ui.Text(c, "服务端没有找到 ffmpeg，现在没法转换上传的文件。用 Docker 镜像运行，或者在服务器上安装 ffmpeg。").TextColor(t.Warning).FontSize(13)
	}
	errorText(c, a.setsErr)
	if a.setsBusy && !a.setsLoaded {
		ui.Row(c).Gap(8).Children(func() {
			ui.Spinner(c)
			muted(c, "正在读取贴纸包…")
		})
		return
	}
	if a.setsLoaded && len(a.sets) == 0 {
		panel(c).AlignItems(ui.Center).Padding(40).Children(func() {
			ui.Text(c, "还没有贴纸包").FontSize(18).Bold()
			muted(c, "新建一个，把手头的 GIF 拖进来就能变成视频贴纸。")
			iconButton(c, "new-set-empty", "新建贴纸包", iconPlus, true).OnClick(a.startCreate)
		})
		return
	}
	ui.Grid(c).Columns(a.gridColumns(200)).Gap(14).Children(func() {
		for _, s := range a.sets {
			ui.ButtonBase(c.Key("set-"+s.ShortName)).Padding(14).Gap(10).Radius(14).Background(t.Surface).Border(1, t.Border).
				AlignItems(ui.Center).Label(s.Title).OnClick(func() { a.openSet(s.ShortName) }).Children(func() {
				a.thumbBox(c, s.Cover, false, 96)
				ui.Text(c, s.Title).Bold().SingleLine().Ellipsis("…").Tooltip(s.Title)
				ui.Row(c).Gap(6).Children(func() {
					muted(c, strconv.Itoa(s.Count)+" 张").FontSize(12)
					if s.Emojis {
						pill(c, "表情", t.Accent)
					}
					if s.Archived {
						pill(c, "已归档", t.TextMuted)
					}
				})
			})
		}
	})
}

/* 一个贴纸包 */

func (a *app) setView(c *ui.Context) {
	t := c.Theme()
	s := a.set
	ui.Row(c).Gap(8).Children(func() {
		iconButton(c, "back", "全部贴纸包", iconBack, false).OnClick(a.closeSet)
	})
	if s == nil {
		errorText(c, a.setErr)
		if a.setBusy {
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				muted(c, "正在读取贴纸…")
			})
		}
		return
	}
	link := "https://t.me/addstickers/" + s.ShortName
	ui.Row(c).Gap(12).AlignItems(ui.End).Wrap().Children(func() {
		ui.Column(c).Gap(4).Grow(1).MinWidth(240).Children(func() {
			ui.Text(c, s.Title).FontSize(26).Bold().Selectable()
			ui.Row(c).Gap(8).Children(func() {
				muted(c, strconv.Itoa(len(s.Stickers))+" 张 ·").FontSize(13)
				ui.Link(c, "t.me/addstickers/"+s.ShortName, link)
			})
		})
		ui.Row(c).Gap(8).Children(func() {
			iconButton(c, "refresh-set", "刷新", iconRefresh, false).Disabled(a.setBusy).OnClick(func() { a.openSet(s.ShortName) })
			ui.Button(c.Key("rename"), "改名").Disabled(a.busy["rename"]).OnClick(func() {
				a.rename.open, a.rename.title = true, s.Title
			})
			ui.Button(c.Key("delete-set"), "删除贴纸包").Disabled(a.busy["delete-set"]).OnClick(a.deleteSet)
		})
	})
	errorText(c, a.setErr)
	if s.Emojis || s.Masks {
		ui.Text(c, "这是自定义表情包或面具贴纸包，可以查看和整理，但上传只支持普通贴纸包。").TextColor(t.Warning).FontSize(13)
	}
	a.uploadPanel(c, s.ShortName, !s.Emojis && !s.Masks)
	a.selectionBar(c, s)
	a.stickerGrid(c, s)
}

// uploadPanel 是添加贴纸的地方，选项在选择或拖入文件时保存到上传队列。
func (a *app) uploadPanel(c *ui.Context, target string, enabled bool) {
	t := c.Theme()
	zone := panel(c).Gap(12)
	if files := zone.DroppedFiles(); files != nil && enabled {
		a.enqueue(files, target)
	}
	if zone.FileDragOver() && enabled {
		zone.Border(2, t.Accent).Background(t.Accent.Alpha(0.06))
	}
	zone.Children(func() {
		ui.Row(c).Gap(12).Wrap().AlignItems(ui.End).Children(func() {
			ui.Column(c).Gap(4).Grow(1).MinWidth(220).Children(func() {
				ui.Text(c, "添加贴纸").Bold()
				muted(c, "把 GIF、PNG、JPG、WEBP、MP4 拖到这里，或者点右边选择。动图会转换成视频贴纸（WEBM，长边 512，最长 3 秒，不超过 256 KB），静态图转换成静态贴纸。").FontSize(12)
			})
			iconButton(c, "pick-stickers", "选择文件", iconUpload, true).Disabled(!enabled).OnClick(func() { a.pickFiles(target) })
		})
		a.uploadOptions(c, enabled)
	})
}

func (a *app) uploadOptions(c *ui.Context, enabled bool) {
	ui.Row(c).Gap(16).Wrap().Children(func() {
		ui.Field(c, "表情（这批贴纸都用它）", func() {
			ui.TextInput(c.Key("upload-emoji"), &a.emoji).Placeholder("🙂").Width(160).Disabled(!enabled)
		})
		ui.Field(c, "动图超过 3 秒时", func() {
			if ui.Segmented(c.Key("fit"), &a.fit, "加速到 3 秒", "只取前 3 秒").Disabled(!enabled).Changed() {
				_ = a.savePrefs()
			}
		})
	})
	a.mediaFields(c, &a.media, enabled, true)
}

// selectionBar 是选中一张贴纸后出现的操作条
func (a *app) selectionBar(c *ui.Context, s *api.Set) {
	i := slices.IndexFunc(s.Stickers, func(st api.Sticker) bool { return st.ID == a.selected })
	if i < 0 {
		return
	}
	st, busy, last := s.Stickers[i], a.busy["sticker"], len(s.Stickers)-1
	t := c.Theme()
	ui.Row(c).Gap(10).Padding(10, 14).Radius(12).Background(t.Accent.Alpha(0.1)).Wrap().Children(func() {
		ui.Text(c, fmt.Sprintf("第 %d 张", i+1)).Bold()
		ui.TextInput(c.Key("edit-emoji-"+strconv.FormatInt(st.ID, 10)), &a.editEmoji).Width(120).Label("表情").Disabled(busy).
			OnSubmit(func() { a.saveEmoji(st.ID, a.editEmoji) })
		ui.Button(c.Key("save-emoji"), "改表情").Disabled(busy || a.editEmoji == "" || a.editEmoji == st.Emoji).
			OnClick(func() { a.saveEmoji(st.ID, a.editEmoji) })
		ui.Spacer(c)
		ui.Button(c.Key("move-first"), "移到最前").Disabled(busy || i == 0).OnClick(func() { a.moveSticker(st.ID, 0) })
		iconButton(c, "move-left", "前移", iconLeft, false).Disabled(busy || i == 0).OnClick(func() { a.moveSticker(st.ID, i-1) })
		iconButton(c, "move-right", "后移", iconRight, false).Disabled(busy || i == last).OnClick(func() { a.moveSticker(st.ID, i+1) })
		ui.Button(c.Key("move-last"), "移到最后").Disabled(busy || i == last).OnClick(func() { a.moveSticker(st.ID, last) })
		iconButton(c, "delete-sticker", "删除", iconTrash, false).Disabled(busy).OnClick(func() { a.deleteSticker(st.ID) })
	})
}

func kindLabel(k string) string {
	switch k {
	case api.KindVideo:
		return "视频"
	case api.KindAnimated:
		return "动画"
	}
	return "静态"
}

func (a *app) stickerGrid(c *ui.Context, s *api.Set) {
	t := c.Theme()
	if len(s.Stickers) == 0 {
		muted(c, "贴纸包是空的")
		return
	}
	muted(c, "点一张贴纸可以改表情、调整顺序或删除；右键也有这些操作。").FontSize(12)
	last := len(s.Stickers) - 1
	ui.Grid(c).Columns(a.gridColumns(132)).Gap(10).Children(func() {
		for i, st := range s.Stickers {
			sel := st.ID == a.selected
			bg, border := t.Surface, t.Border
			if sel {
				bg, border = t.Accent.Alpha(0.12), t.Accent
			}
			cell := ui.ButtonBase(c.Key("sticker-"+strconv.FormatInt(st.ID, 10))).Padding(10).Gap(6).Radius(12).
				Background(bg).Border(1, border).AlignItems(ui.Center).Label("贴纸 " + st.Emoji).
				OnClick(func() {
					if a.selected == st.ID {
						a.selected = 0
						return
					}
					a.selected, a.editEmoji = st.ID, st.Emoji
				})
			cell.ContextMenu(func(m *ui.Menu) {
				busy := a.busy["sticker"]
				if m.Item("移到最前").Disabled(busy || i == 0).Chosen() {
					a.moveSticker(st.ID, 0)
				}
				if m.Item("前移").Disabled(busy || i == 0).Chosen() {
					a.moveSticker(st.ID, i-1)
				}
				if m.Item("后移").Disabled(busy || i == last).Chosen() {
					a.moveSticker(st.ID, i+1)
				}
				if m.Item("移到最后").Disabled(busy || i == last).Chosen() {
					a.moveSticker(st.ID, last)
				}
				m.Separator()
				if m.Item("删除").Danger().Disabled(busy).Chosen() {
					a.deleteSticker(st.ID)
				}
			})
			cell.Children(func() {
				a.thumbBox(c, st.ID, false, 96)
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, st.Emoji).FontSize(15).SingleLine()
					muted(c, kindLabel(st.Kind)).FontSize(11)
				})
			})
		}
	})
}

/* 新建贴纸包 */

func (a *app) createDialog(c *ui.Context) {
	f := &a.create
	ui.Modal(c, &f.open, func() {
		ui.Scroll(c.Key("create-scroll")).Width(min(560, a.width-64)).MaxHeight(560).Children(func() {
			ui.Column(c).FillWidth().Gap(14).Padding(8).Children(func() {
				ui.Text(c, "新建贴纸包").Bold().FontSize(20)
				ui.Field(c, "名字（别人添加时看到的）", func() {
					ui.TextInput(c.Key("create-title"), &f.title).Placeholder("我的表情").MaxLength(64).Disabled(f.busy)
				})
				ui.Field(c, "链接名（t.me/addstickers/后面那段，创建后不能改）", func() {
					ui.Row(c).Gap(8).Children(func() {
						in := ui.TextInput(c.Key("create-short"), &f.shortName).Placeholder("my_stickers").MaxLength(64).Grow(1).Disabled(f.busy)
						if in.Changed() {
							f.shortOK, f.shortReason, f.checkedAs = nil, "", ""
						}
						ui.Button(c.Key("check-short"), "检查").Disabled(f.busy || f.checking || f.shortName == "").OnClick(a.checkShortName)
					})
				})
				switch {
				case f.checking:
					muted(c, "正在检查…").FontSize(12)
				case f.shortOK != nil && *f.shortOK:
					ui.Text(c, "可以用").TextColor(c.Theme().Success).FontSize(12)
				case f.shortOK != nil:
					ui.Text(c, f.shortReason).TextColor(c.Theme().Danger).FontSize(12)
				default:
					muted(c, "只能用英文字母、数字和下划线，以字母开头。").FontSize(12)
				}
				a.uploadOptions(c, !f.busy)
				ui.Row(c).Gap(8).Children(func() {
					ui.Button(c.Key("create-pick"), "选择文件").Disabled(f.busy).OnClick(a.pickCreateFiles)
					if len(f.files) > 0 {
						ui.Button(c.Key("create-clear"), "清空").Disabled(f.busy).OnClick(func() { f.files = nil })
					}
				})
				zone := ui.Column(c).Gap(4).Padding(10).Radius(10).Border(1, c.Theme().Border).MinHeight(64)
				if files := zone.DroppedFiles(); files != nil && !f.busy {
					f.files = append(f.files, a.checkFiles(files)...)
				}
				if zone.FileDragOver() {
					zone.Border(2, c.Theme().Accent)
				}
				zone.Children(func() {
					if len(f.files) == 0 {
						muted(c, "把图片或动图拖到这里。第一张用来创建贴纸包，其余的创建后自动加进去。").FontSize(12)
					}
					for i, p := range f.files {
						label := filepath.Base(p)
						if i == 0 {
							label += "（第一张）"
						}
						ui.Text(c, label).FontSize(12).SingleLine().Ellipsis("…")
					}
				})
				errorText(c, f.err)
				ui.Row(c).Gap(8).Children(func() {
					ui.PrimaryButton(c.Key("create-ok"), "创建").Disabled(f.busy).OnClick(a.submitCreate)
					ui.Button(c.Key("create-cancel"), "取消").Disabled(f.busy).OnClick(func() { f.open = false })
					if f.busy {
						ui.Spinner(c)
						muted(c, "正在转换并上传第一张…").FontSize(12)
					}
				})
			})
		})
	})
}

func (a *app) renameDialog(c *ui.Context) {
	ui.Modal(c, &a.rename.open, func() {
		ui.Column(c).Width(min(400, a.width-64)).Gap(14).Padding(8).Children(func() {
			ui.Text(c, "改名").Bold().FontSize(20)
			ui.TextInput(c.Key("rename-title"), &a.rename.title).MaxLength(64).AutoFocus().OnSubmit(a.renameSet)
			muted(c, "链接名不能改。").FontSize(12)
			ui.Row(c).Gap(8).Children(func() {
				ui.PrimaryButton(c.Key("rename-ok"), "保存").Disabled(a.rename.title == "").OnClick(a.renameSet)
				ui.Button(c.Key("rename-cancel"), "取消").OnClick(func() { a.rename.open = false })
			})
		})
	})
}

/* 上传队列：页面底部，有文件在排队或刚传完时出现 */

func (a *app) queueBar(c *ui.Context) {
	if len(a.queue) == 0 {
		return
	}
	t := c.Theme()
	done, failed := 0, 0
	for _, j := range a.queue {
		switch j.state {
		case jobDone:
			done++
		case jobFailed:
			failed++
		}
	}
	ui.Column(c).Padding(12, 24).Gap(8).Background(t.Surface).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).MaxHeight(220).Children(func() {
		ui.Row(c).Gap(10).Children(func() {
			ui.Text(c, fmt.Sprintf("上传 %d / %d", done, len(a.queue))).Bold()
			if failed > 0 {
				pill(c, fmt.Sprintf("%d 个失败", failed), t.Danger)
			}
			ui.Progress(c, float64(done+failed)/float64(len(a.queue))).Grow(1)
			ui.Button(c.Key("queue-clear"), "清除已完成").Disabled(done == 0).OnClick(a.clearFinished)
		})
		ui.Scroll(c.Key("queue")).MinHeight(0).Grow(1).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				for i, j := range a.queue {
					ui.Row(c.Key(i)).Gap(10).Children(func() {
						switch j.state {
						case jobRunning:
							ui.Spinner(c).Size(14, 14)
						case jobDone:
							ui.Text(c, "✓").TextColor(t.Success)
						case jobFailed:
							ui.Text(c, "✕").TextColor(t.Danger)
						default:
							muted(c, "·")
						}
						ui.Text(c, filepath.Base(j.path)).FontSize(12).SingleLine().Ellipsis("…").MaxWidth(280)
						where := "GIF 收藏"
						if j.target != "" {
							where = j.target
						}
						muted(c, "→ "+where).FontSize(12).SingleLine()
						if j.err != "" {
							ui.Text(c, j.err).FontSize(12).TextColor(t.Danger).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(j.err).Selectable()
							ui.Button(c.Key(fmt.Sprintf("retry-%d", i)), "重试").OnClick(func() { a.retry(j) })
						}
					})
				}
			})
		})
	})
}
