package main

import (
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	var win *mygo.Window
	a := newApp(systemStore{}, func(fn func()) { win.Update(fn) })
	a.openFiles = func(title string, gifOnly bool) ([]string, error) {
		f := mygo.FileFilter{Name: "图片和动图", Extensions: []string{"gif", "png", "jpg", "jpeg", "webp", "webm", "mp4", "mov"}}
		if gifOnly {
			f = mygo.FileFilter{Name: "动图", Extensions: []string{"gif", "webp", "webm", "mp4", "mov"}}
		}
		return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: title, Filters: []mygo.FileFilter{f}, Multiple: true})
	}
	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{Title: "TG Stickers", Width: 1080, Height: 760, MinWidth: 480, MinHeight: 560, StateKey: "main", Content: ui.View(a.view)})
		a.autoConnect()
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				win.Update(a.tick)
			}
		}()
	})
	mygo.App.OnQuit(func() { a.cancel() })
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
