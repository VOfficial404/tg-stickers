<div align="center">

<img src="app/resources/icon.png" width="96" alt="TG Stickers">

# TG Stickers

**用自己的 Telegram 账号管理贴纸包，GIF 拖进去就是视频贴纸。**

[![CI](https://img.shields.io/github/actions/workflow/status/VOfficial404/tg-stickers/ci.yml?label=CI)](https://github.com/VOfficial404/tg-stickers/actions/workflows/ci.yml)
[![版本](https://img.shields.io/github/v/release/VOfficial404/tg-stickers?label=版本)](https://github.com/VOfficial404/tg-stickers/releases/latest)

</div>

在 Telegram 里做视频贴纸，要先把 GIF 转成 VP9 编码、长边 512、不超过 3 秒、不超过 256 KB 的 WEBM，再一张张发给 @Stickers 机器人。这个项目把这些都省了：

- **不用 Bot。** 服务端用你自己的账号（MTProto 用户 API）登录，看到和管理的就是你账号创建的全部贴纸包，包括以前通过 @Stickers 做的。
- **GIF 直接变贴纸。** 一次拖进一堆 GIF、MP4、PNG，服务端用 ffmpeg 转换好依次传上去。超过 3 秒的动图可以选加速到 3 秒或只取前 3 秒；画面太复杂时自动降低画质和帧率，直到不超过 256 KB。
- **贴纸包随手整理。** 新建、改名、删除贴纸包；贴纸改表情、调顺序、删除。
- **GIF 收藏也能管。** 把动图存进 Telegram 的「GIF」标签页，或者从里面移除。
- **跑在你的服务器上。** 服务端放在 VPS 上常驻，转换、上传都在服务器上做；电脑上只开一个客户端连过去。

## 组成

| | |
|---|---|
| **服务端** `tg-stickers` | Go 写的，登录 Telegram、转换文件、提供接口。推荐用 Docker 运行，镜像自带 ffmpeg |
| **桌面客户端** | 用 [MyGo](https://github.com/egoist/mygo) 写的原生界面，Windows、macOS、Linux 都有，连接服务端使用 |

都在 GitHub Actions 上编译，从 [Releases](https://github.com/VOfficial404/tg-stickers/releases/latest) 下载。

## 开始用

### 1. 申请 api_id 和 api_hash

用要登录的账号打开 [my.telegram.org](https://my.telegram.org/apps)，进入 **API development tools**，随便填个应用名创建，记下 `api_id`（数字）和 `api_hash`（32 位）。这是 Telegram 要求第三方客户端提供的，等于这个程序的「身份证」，别给别人。

### 2. 在服务器上运行服务端

一台装了 Docker 的 Linux 服务器：

```bash
mkdir -p /opt/tg-stickers && cd /opt/tg-stickers
curl -fsSLO https://raw.githubusercontent.com/VOfficial404/tg-stickers/main/compose.yaml
docker compose up -d
docker compose exec tg-stickers tg-stickers token   # 客户端连接时要填的令牌
```

更新：`docker compose pull && docker compose up -d`。

> [!IMPORTANT]
> 服务端默认只听服务器本机的 8787 端口。从自己电脑连过去用 SSH 隧道：
> `ssh -N -L 8787:127.0.0.1:8787 root@服务器IP`，保持运行，客户端里填 `http://127.0.0.1:8787`。
> 有域名的话也可以用 HTTPS 反向代理（例如 Caddy 的 `reverse_proxy 127.0.0.1:8787`）。**别把明文端口开到公网**：拿到令牌就能操作你的 Telegram 贴纸。

### 3. 打开客户端登录

客户端填服务器地址和令牌，然后按提示依次填 api_id / api_hash、手机号、Telegram 发来的验证码，开了两步验证的再填密码。登录后的会话保存在服务器的 `data/session.json`，之后重启也不用再登录。

## 配置

都用环境变量，Docker 里写在 `compose.yaml` 的 `environment` 下：

| 变量 | 说明 |
|---|---|
| `TG_STICKERS_TOKEN` | 客户端连接用的令牌。不设时自动生成，保存在数据目录 |
| `TG_STICKERS_API_ID` / `TG_STICKERS_API_HASH` | 设了就不用在客户端里填 |
| `TG_STICKERS_PROXY` | 服务器连不上 Telegram 时用的 SOCKS5 代理，例如 `socks5://127.0.0.1:1080` |
| `TG_STICKERS_ADDR` | 监听地址，直接运行时默认 `127.0.0.1:8787` |
| `TG_STICKERS_DATA` | 数据目录，直接运行时默认是程序旁边的 `data` |
| `TG_STICKERS_FFMPEG` | ffmpeg 的路径，默认在 PATH 里找 |

不用 Docker 时，从 Releases 下载 `tg-stickers_linux_amd64` 之类的文件直接运行，服务器上要装好带 libvpx、libx264、libwebp 的 ffmpeg（Debian / Ubuntu 的 `apt install ffmpeg` 就是）。

## 先读这些

- **数据目录就是你的账号。** `data/session.json` 是 Telegram 的登录会话，拿到它就等于登录了你的账号。别泄露、别提交到 Git。不用了在客户端「账号」页退出登录，或者在 Telegram 的「设置 → 设备」里结束名为 `tg-stickers` 的会话。
- **只做普通操作。** 用的都是 Telegram 官方客户端也在用的贴纸接口，正常频率使用不会有问题；别拿它批量刷几百个贴纸包。Telegram 要求等待时，客户端会提示要等多久。
- **视频贴纸的限制来自 Telegram**：WEBM（VP9）、长边 512、最长 3 秒、最多 30 帧/秒、不超过 256 KB、没有声音。普通贴纸包最多 120 张。
- **能看不能做的**：自定义表情包和面具贴纸包可以查看、调整顺序、删除，但上传只支持普通贴纸包；TGS（Lottie）动画贴纸能管理，不能从这里制作。

## 开发

```
api/          接口的数据类型和客户端（桌面客户端只依赖它）
account/      Telegram 登录、贴纸包和 GIF 收藏的操作（gotd/td）
convert/      ffmpeg 转换
server/       HTTP 接口
cmd/          服务端入口
app/          MyGo 桌面客户端（单独的 Go 模块）
```

编译都在 GitHub Actions 上：推到 `main` 跑 CI（含真实 ffmpeg 的转换测试）并推送 Docker 镜像；打 `v*` 标签时编译服务端和三个平台的客户端，发布到 Releases。发版前改 `app/mygo.json` 和 `app/app.go` 里的版本号，并在 `CHANGELOG.md` 写一段 `## v版本号`。
