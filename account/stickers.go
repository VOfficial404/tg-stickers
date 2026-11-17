package account

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/VOfficial404/tg-stickers/api"
	"github.com/VOfficial404/tg-stickers/convert"
)

func (a *Account) remember(d *tg.Document, set string) {
	a.docsMu.Lock()
	a.docs[d.ID] = docRef{doc: d, set: set}
	a.docsMu.Unlock()
}

func (a *Account) lookup(id int64) (docRef, bool) {
	a.docsMu.Lock()
	defer a.docsMu.Unlock()
	r, ok := a.docs[id]
	return r, ok
}

func (a *Account) forget(id int64) {
	a.docsMu.Lock()
	delete(a.docs, id)
	a.docsMu.Unlock()
}

// sticker 找到一张贴纸；服务端重启后没见过时让客户端刷新
func (a *Account) sticker(id int64) (docRef, error) {
	r, ok := a.lookup(id)
	if !ok || r.set == "" {
		return docRef{}, notFound("找不到这张贴纸，刷新一下贴纸包")
	}
	return r, nil
}

func shortNameInput(name string) *tg.InputStickerSetShortName {
	return &tg.InputStickerSetShortName{ShortName: name}
}

func summary(s tg.StickerSet) api.SetSummary {
	return api.SetSummary{
		ID: s.ID, ShortName: s.ShortName, Title: s.Title, Count: s.Count,
		Emojis: s.Emojis, Masks: s.Masks, Archived: s.Archived,
	}
}

// Sets 列出账号创建的全部贴纸包
func (a *Account) Sets(ctx context.Context) ([]api.SetSummary, error) {
	client, err := a.ready()
	if err != nil {
		return nil, err
	}
	out := []api.SetSummary{}
	var offset int64
	for {
		res, err := client.API().MessagesGetMyStickers(ctx, &tg.MessagesGetMyStickersRequest{OffsetID: offset, Limit: 100})
		if err != nil {
			return nil, a.loggedOut(err)
		}
		for _, c := range res.Sets {
			set := c.GetSet()
			s := summary(set)
			var cover tg.DocumentClass
			switch c := c.(type) {
			case *tg.StickerSetCovered:
				cover = c.Cover
			case *tg.StickerSetMultiCovered:
				if len(c.Covers) > 0 {
					cover = c.Covers[0]
				}
			case *tg.StickerSetFullCovered:
				if len(c.Documents) > 0 {
					cover = c.Documents[0]
				}
			}
			if d, ok := cover.(*tg.Document); ok {
				a.remember(d, set.ShortName)
				s.Cover = d.ID
			}
			out = append(out, s)
			offset = set.ID
		}
		if len(res.Sets) == 0 || len(out) >= res.Count {
			return out, nil
		}
	}
}

// Set 取一个贴纸包的全部贴纸
func (a *Account) Set(ctx context.Context, shortName string) (api.Set, error) {
	client, err := a.ready()
	if err != nil {
		return api.Set{}, err
	}
	res, err := client.API().MessagesGetStickerSet(ctx, &tg.MessagesGetStickerSetRequest{Stickerset: shortNameInput(shortName)})
	if err != nil {
		return api.Set{}, a.loggedOut(err)
	}
	return a.toSet(res)
}

// toSet 把 Telegram 返回的贴纸包换成接口的格式，顺便记住里面的文件
func (a *Account) toSet(res tg.MessagesStickerSetClass) (api.Set, error) {
	full, ok := res.(*tg.MessagesStickerSet)
	if !ok {
		return api.Set{}, errors.New("Telegram 没有返回贴纸包的内容")
	}
	emoji := map[int64]string{}
	for _, p := range full.Packs {
		for _, id := range p.Documents {
			emoji[id] += p.Emoticon
		}
	}
	set := api.Set{SetSummary: summary(full.Set), Stickers: []api.Sticker{}}
	for _, dc := range full.Documents {
		d, ok := dc.(*tg.Document)
		if !ok {
			continue
		}
		a.remember(d, full.Set.ShortName)
		if set.Cover == 0 {
			set.Cover = d.ID
		}
		set.Stickers = append(set.Stickers, api.Sticker{ID: d.ID, Emoji: emoji[d.ID], Kind: kind(d.MimeType), Size: d.Size})
	}
	set.Count = len(set.Stickers)
	return set, nil
}

func kind(mime string) string {
	switch mime {
	case "video/webm":
		return api.KindVideo
	case "application/x-tgsticker":
		return api.KindAnimated
	}
	return api.KindStatic
}

// CheckShortName 查询贴纸包链接名能不能用
func (a *Account) CheckShortName(ctx context.Context, name string) (api.ShortNameCheck, error) {
	client, err := a.ready()
	if err != nil {
		return api.ShortNameCheck{}, err
	}
	ok, err := client.API().StickersCheckShortName(ctx, name)
	if err != nil {
		if tgerr.Is(err, "SHORT_NAME_INVALID", "SHORT_NAME_OCCUPIED") {
			return api.ShortNameCheck{Reason: humanize(err).Error()}, nil
		}
		return api.ShortNameCheck{}, a.loggedOut(err)
	}
	if !ok {
		return api.ShortNameCheck{Reason: tgMessages["SHORT_NAME_OCCUPIED"]}, nil
	}
	return api.ShortNameCheck{Available: true}, nil
}

// upload 把转换好的贴纸传到 Telegram，拿到可以放进贴纸包的文件
func (a *Account) upload(ctx context.Context, client *telegram.Client, path, mime, name string, attrs ...tg.DocumentAttributeClass) (*tg.Document, error) {
	file, err := uploader.NewUploader(client.API()).FromPath(ctx, path)
	if err != nil {
		return nil, a.loggedOut(err)
	}
	media, err := client.API().MessagesUploadMedia(ctx, &tg.MessagesUploadMediaRequest{
		Peer: &tg.InputPeerSelf{},
		Media: &tg.InputMediaUploadedDocument{
			File:       file,
			MimeType:   mime,
			Attributes: append([]tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: name}}, attrs...),
		},
	})
	if err != nil {
		return nil, a.loggedOut(err)
	}
	m, ok := media.(*tg.MessageMediaDocument)
	if !ok {
		return nil, errors.New("Telegram 没有把上传的文件当作文档")
	}
	d, ok := m.Document.(*tg.Document)
	if !ok {
		return nil, errors.New("Telegram 没有返回上传的文件")
	}
	return d, nil
}

// cleanEmoji 去掉表情里的空白；没填时用 🙂
func cleanEmoji(s string) string {
	s = strings.Join(strings.Fields(s), "")
	if s == "" {
		return "🙂"
	}
	return s
}

// CreateSet 用一张转换好的贴纸新建贴纸包
func (a *Account) CreateSet(ctx context.Context, title, shortName, emoji string, st convert.Sticker) (api.Set, error) {
	title, shortName = strings.TrimSpace(title), strings.TrimSpace(shortName)
	if title == "" || len([]rune(title)) > 64 {
		return api.Set{}, userError(tgMessages["PACK_TITLE_INVALID"])
	}
	if shortName == "" || len(shortName) > 64 {
		return api.Set{}, userError(tgMessages["SHORT_NAME_INVALID"])
	}
	client, err := a.ready()
	if err != nil {
		return api.Set{}, err
	}
	d, err := a.upload(ctx, client, st.Path, st.Mime, st.Name)
	if err != nil {
		return api.Set{}, err
	}
	res, err := client.API().StickersCreateStickerSet(ctx, &tg.StickersCreateStickerSetRequest{
		UserID:    &tg.InputUserSelf{},
		Title:     title,
		ShortName: shortName,
		Stickers:  []tg.InputStickerSetItem{{Document: d.AsInput(), Emoji: cleanEmoji(emoji)}},
		Software:  "tg-stickers",
	})
	if err != nil {
		return api.Set{}, a.loggedOut(err)
	}
	a.cfg.Logf("新建贴纸包 %s（%s）", title, shortName)
	return a.toSet(res)
}

// AddSticker 把一张转换好的贴纸加到贴纸包末尾
func (a *Account) AddSticker(ctx context.Context, shortName, emoji string, st convert.Sticker) (api.Set, error) {
	client, err := a.ready()
	if err != nil {
		return api.Set{}, err
	}
	d, err := a.upload(ctx, client, st.Path, st.Mime, st.Name)
	if err != nil {
		return api.Set{}, err
	}
	res, err := client.API().StickersAddStickerToSet(ctx, &tg.StickersAddStickerToSetRequest{
		Stickerset: shortNameInput(shortName),
		Sticker:    tg.InputStickerSetItem{Document: d.AsInput(), Emoji: cleanEmoji(emoji)},
	})
	if err != nil {
		return api.Set{}, a.loggedOut(err)
	}
	return a.toSet(res)
}

// RenameSet 改贴纸包的名字（链接名不能改）
func (a *Account) RenameSet(ctx context.Context, shortName, title string) (api.Set, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 64 {
		return api.Set{}, userError(tgMessages["PACK_TITLE_INVALID"])
	}
	client, err := a.ready()
	if err != nil {
		return api.Set{}, err
	}
	res, err := client.API().StickersRenameStickerSet(ctx, &tg.StickersRenameStickerSetRequest{Stickerset: shortNameInput(shortName), Title: title})
	if err != nil {
		return api.Set{}, a.loggedOut(err)
	}
	return a.toSet(res)
}

// DeleteSet 删除整个贴纸包，Telegram 上也就没有了
func (a *Account) DeleteSet(ctx context.Context, shortName string) error {
	client, err := a.ready()
	if err != nil {
		return err
	}
	if _, err := client.API().StickersDeleteStickerSet(ctx, shortNameInput(shortName)); err != nil {
		return a.loggedOut(err)
	}
	a.cfg.Logf("删除贴纸包 %s", shortName)
	return nil
}

// withSticker 对一张贴纸做 op；文件引用过期时重新取一次贴纸包再试
func (a *Account) withSticker(ctx context.Context, id int64, op func(*tg.Client, *tg.InputDocument) (tg.MessagesStickerSetClass, error)) (api.Set, error) {
	client, err := a.ready()
	if err != nil {
		return api.Set{}, err
	}
	r, err := a.sticker(id)
	if err != nil {
		return api.Set{}, err
	}
	res, err := op(client.API(), r.doc.AsInput())
	if tgerr.Is(err, "FILE_REFERENCE_EXPIRED") {
		if _, err := a.Set(ctx, r.set); err != nil {
			return api.Set{}, err
		}
		if r, err = a.sticker(id); err != nil {
			return api.Set{}, err
		}
		res, err = op(client.API(), r.doc.AsInput())
	}
	if err != nil {
		return api.Set{}, a.loggedOut(err)
	}
	return a.toSet(res)
}

// DeleteSticker 从贴纸包里删掉一张贴纸
func (a *Account) DeleteSticker(ctx context.Context, id int64) (api.Set, error) {
	set, err := a.withSticker(ctx, id, func(c *tg.Client, d *tg.InputDocument) (tg.MessagesStickerSetClass, error) {
		return c.StickersRemoveStickerFromSet(ctx, d)
	})
	if err == nil {
		a.forget(id)
	}
	return set, err
}

// PatchSticker 改贴纸的表情或位置
func (a *Account) PatchSticker(ctx context.Context, id int64, p api.StickerPatch) (api.Set, error) {
	if p.Emoji == "" && p.Position == nil {
		return api.Set{}, userError("没有要改的")
	}
	var set api.Set
	var err error
	if p.Emoji != "" {
		emoji := cleanEmoji(p.Emoji)
		set, err = a.withSticker(ctx, id, func(c *tg.Client, d *tg.InputDocument) (tg.MessagesStickerSetClass, error) {
			return c.StickersChangeSticker(ctx, &tg.StickersChangeStickerRequest{Sticker: d, Emoji: emoji})
		})
		if err != nil {
			return set, err
		}
	}
	if p.Position != nil {
		pos := *p.Position
		if pos < 0 {
			return api.Set{}, userError("位置不对")
		}
		set, err = a.withSticker(ctx, id, func(c *tg.Client, d *tg.InputDocument) (tg.MessagesStickerSetClass, error) {
			return c.StickersChangeStickerPosition(ctx, &tg.StickersChangeStickerPositionRequest{Sticker: d, Position: pos})
		})
	}
	return set, err
}

// GIFs 列出「GIF 收藏」，最近存的在前
func (a *Account) GIFs(ctx context.Context) ([]api.GIF, error) {
	client, err := a.ready()
	if err != nil {
		return nil, err
	}
	res, err := client.API().MessagesGetSavedGifs(ctx, 0)
	if err != nil {
		return nil, a.loggedOut(err)
	}
	saved, ok := res.(*tg.MessagesSavedGifs)
	if !ok {
		return nil, errors.New("Telegram 没有返回 GIF 收藏")
	}
	out := []api.GIF{}
	for _, dc := range saved.Gifs {
		d, ok := dc.(*tg.Document)
		if !ok {
			continue
		}
		a.remember(d, "")
		g := api.GIF{ID: d.ID, Size: d.Size}
		for _, at := range d.Attributes {
			if v, ok := at.(*tg.DocumentAttributeVideo); ok {
				g.W, g.H = v.W, v.H
			}
		}
		out = append(out, g)
	}
	return out, nil
}

// AddGIF 上传转换好的 MP4 并存进「GIF 收藏」
func (a *Account) AddGIF(ctx context.Context, g convert.GIF) ([]api.GIF, error) {
	client, err := a.ready()
	if err != nil {
		return nil, err
	}
	d, err := a.upload(ctx, client, g.Path, "video/mp4", "animation.mp4",
		&tg.DocumentAttributeVideo{SupportsStreaming: true, Nosound: true, Duration: g.Duration, W: g.W, H: g.H},
		&tg.DocumentAttributeAnimated{})
	if err != nil {
		return nil, err
	}
	if _, err := client.API().MessagesSaveGif(ctx, &tg.MessagesSaveGifRequest{ID: d.AsInput()}); err != nil {
		return nil, a.loggedOut(err)
	}
	return a.GIFs(ctx)
}

// DeleteGIF 从「GIF 收藏」里拿掉一个
func (a *Account) DeleteGIF(ctx context.Context, id int64) ([]api.GIF, error) {
	client, err := a.ready()
	if err != nil {
		return nil, err
	}
	r, ok := a.lookup(id)
	if !ok || r.set != "" {
		return nil, notFound("找不到这个 GIF，刷新一下")
	}
	if _, err := client.API().MessagesSaveGif(ctx, &tg.MessagesSaveGifRequest{ID: r.doc.AsInput(), Unsave: true}); err != nil {
		return nil, a.loggedOut(err)
	}
	a.forget(id)
	return a.GIFs(ctx)
}

// Thumb 返回贴纸或 GIF 的缩略图，缓存在数据目录的 thumbs 里。文件不会变，缓存不用过期。
func (a *Account) Thumb(ctx context.Context, id int64, ff *convert.Tool) ([]byte, error) {
	dir := filepath.Join(a.cfg.DataDir, "thumbs")
	cached := filepath.Join(dir, strconv.FormatInt(id, 10))
	if b, err := os.ReadFile(cached); err == nil {
		return b, nil
	}
	client, err := a.ready()
	if err != nil {
		return nil, err
	}
	r, ok := a.lookup(id)
	if !ok {
		return nil, notFound("找不到这个文件，刷新一下")
	}
	b, err := a.thumb(ctx, client, r.doc, ff)
	if tgerr.Is(err, "FILE_REFERENCE_EXPIRED") {
		if r.set != "" {
			_, err = a.Set(ctx, r.set)
		} else {
			_, err = a.GIFs(ctx)
		}
		if err != nil {
			return nil, err
		}
		if r, ok = a.lookup(id); !ok {
			return nil, notFound("这个文件已经不在了，刷新一下")
		}
		b, err = a.thumb(ctx, client, r.doc, ff)
	}
	if err != nil {
		return nil, a.loggedOut(err)
	}
	if err := os.MkdirAll(dir, 0o700); err == nil {
		_ = os.WriteFile(cached, b, 0o600)
	}
	return b, nil
}

func (a *Account) thumb(ctx context.Context, client *telegram.Client, d *tg.Document, ff *convert.Tool) ([]byte, error) {
	// Telegram 生成的缩略图里挑最大的
	type size struct {
		typ   string
		area  int
		bytes []byte
	}
	var sizes []size
	for _, t := range d.Thumbs {
		switch t := t.(type) {
		case *tg.PhotoSize:
			sizes = append(sizes, size{typ: t.Type, area: t.W * t.H})
		case *tg.PhotoSizeProgressive:
			sizes = append(sizes, size{typ: t.Type, area: t.W * t.H})
		case *tg.PhotoCachedSize:
			sizes = append(sizes, size{typ: t.Type, area: t.W * t.H, bytes: t.Bytes})
		}
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].area > sizes[j].area })
	if len(sizes) > 0 {
		if sizes[0].bytes != nil {
			return sizes[0].bytes, nil
		}
		return a.download(ctx, client, d, sizes[0].typ)
	}
	switch {
	case d.MimeType == "image/webp" || d.MimeType == "image/png" || d.MimeType == "image/jpeg":
		return a.download(ctx, client, d, "")
	case strings.HasPrefix(d.MimeType, "video/") && ff != nil:
		data, err := a.download(ctx, client, d, "")
		if err != nil {
			return nil, err
		}
		tmp, err := os.MkdirTemp("", "tg-stickers-thumb-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		in := filepath.Join(tmp, "in")
		if err := os.WriteFile(in, data, 0o600); err != nil {
			return nil, err
		}
		return ff.Frame(ctx, in, tmp)
	}
	return nil, notFound("这个文件没有缩略图")
}

// download 下载文件或它的一个缩略图（thumbSize 不为空时），到文件所在的数据中心去取
func (a *Account) download(ctx context.Context, client *telegram.Client, d *tg.Document, thumbSize string) ([]byte, error) {
	inv, err := client.FileDC(ctx, d.DCID)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if _, err := downloader.NewDownloader().Download(tg.NewClient(inv), d.AsInputDocumentFileLocation(thumbSize)).Stream(ctx, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
