// tg-stickers 的服务端：用你自己的 Telegram 账号管理贴纸包，把 GIF 转换成视频贴纸。
//
//	tg-stickers [serve]   常驻运行，提供浏览器面板和客户端接口
//	tg-stickers token     显示浏览器或客户端连接时要填的令牌
//	tg-stickers version
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/VOfficial404/tg-stickers/account"
	"github.com/VOfficial404/tg-stickers/api"
	"github.com/VOfficial404/tg-stickers/server"
)

// version 在发布时由 -ldflags "-X main.version=…" 写入
var version = "dev"

const usage = `tg-stickers：用自己的 Telegram 账号管理贴纸包，把 GIF 转换成视频贴纸

用法：
  tg-stickers [serve]   常驻运行，提供浏览器面板和客户端接口
  tg-stickers token     显示浏览器或客户端连接时要填的令牌
  tg-stickers version

环境变量：
  TG_STICKERS_DATA      数据目录，默认是程序所在目录下的 data
  TG_STICKERS_ADDR      监听地址，默认 127.0.0.1:8787（只有本机能连）
  TG_STICKERS_TOKEN     接口令牌，默认自动生成并保存在数据目录里
  TG_STICKERS_API_ID    在 my.telegram.org 申请的 api_id（也可以在浏览器里填）
  TG_STICKERS_API_HASH  在 my.telegram.org 申请的 api_hash
  TG_STICKERS_PROXY     连接 Telegram 用的代理，例如 socks5://127.0.0.1:1080
  TG_STICKERS_FFMPEG    ffmpeg 的路径，默认在 PATH 里找
`

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "token":
		var t string
		if t, err = server.LoadToken(dataDir(), os.Getenv("TG_STICKERS_TOKEN")); err == nil {
			fmt.Println(t)
		}
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "出错了：", err)
		os.Exit(1)
	}
}

func dataDir() string {
	if d := os.Getenv("TG_STICKERS_DATA"); d != "" {
		return d
	}
	exe, err := os.Executable()
	if err != nil {
		return "data"
	}
	return filepath.Join(filepath.Dir(exe), "data")
}

func credentials() (api.Credentials, error) {
	id, hash := strings.TrimSpace(os.Getenv("TG_STICKERS_API_ID")), strings.TrimSpace(os.Getenv("TG_STICKERS_API_HASH"))
	if id == "" && hash == "" {
		return api.Credentials{}, nil
	}
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 || len(hash) != 32 {
		return api.Credentials{}, errors.New("TG_STICKERS_API_ID 要是数字，TG_STICKERS_API_HASH 要是 32 位，两个都要设")
	}
	return api.Credentials{APIID: n, APIHash: hash}, nil
}

// listenURL 是启动日志里提示的地址；监听所有地址时（例如容器里）换成「服务器地址」让人自己填
func listenURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "服务器地址"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func serve() error {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	creds, err := credentials()
	if err != nil {
		return err
	}
	dir := dataDir()
	acc, err := account.New(account.Config{
		DataDir:     dir,
		Credentials: creds,
		Proxy:       os.Getenv("TG_STICKERS_PROXY"),
		Version:     version,
		Logf:        logger.Printf,
	})
	if err != nil {
		return err
	}
	srv, err := server.New(server.Config{
		Account: acc,
		FFmpeg:  os.Getenv("TG_STICKERS_FFMPEG"),
		DataDir: dir,
		Token:   os.Getenv("TG_STICKERS_TOKEN"),
		Version: version,
		Logf:    logger.Printf,
	})
	if err != nil {
		return err
	}
	addr := os.Getenv("TG_STICKERS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	logger.Printf("tg-stickers %s，数据目录 %s", version, dir)
	if v, why := srv.FFmpeg(); v != "" {
		logger.Printf("ffmpeg %s", v)
	} else {
		logger.Printf("没法转换贴纸：%s", why)
	}
	logger.Printf("浏览器打开 %s，输入下面的令牌开始使用；桌面客户端也可连接此地址", listenURL(addr))
	logger.Printf("令牌：%s", srv.Token())

	acc.Start()
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	select {
	case err = <-errc:
	case <-ctx.Done():
		logger.Printf("正在停止")
		sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = hs.Shutdown(sctx)
		scancel()
	}
	acc.Stop()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
