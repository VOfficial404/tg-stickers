<div align="center">

<img src="app/resources/icon.png" width="96" alt="TG Stickers">

# TG Stickers

**用自己的 Telegram 账号管理贴纸包，GIF 拖进去就是视频贴纸。**

[![CI](https://img.shields.io/github/actions/workflow/status/VOfficial404/tg-stickers/ci.yml?label=CI)](https://github.com/VOfficial404/tg-stickers/actions/workflows/ci.yml)
[![版本](https://img.shields.io/github/v/release/VOfficial404/tg-stickers?label=版本)](https://github.com/VOfficial404/tg-stickers/releases/latest)

</div>

在 Telegram 里做视频贴纸，要先把 GIF 转成 VP9 编码、长边 512、不超过 3 秒、不超过 256 KB 的 WEBM，再一张张发给 @Stickers 机器人。这个项目把这些都省了：

- **不用 Bot。** 服务端用你自己的账号（MTProto 用户 API）登录，看到和管理的就是你账号创建的全部贴纸包，包括以前通过 @Stickers 做的。
- **GIF 直接变贴纸。** 一次拖进一堆 GIF、MP4、PNG，服务端用 ffmpeg 转换好依次传上去。先按指定时段剪辑，再对超过 3 秒的动图加速到 3 秒或只取所选时段的前 3 秒；画面太复杂时自动降低画质和帧率，直到不超过 256 KB。
- **贴纸包随手整理。** 新建、改名、删除贴纸包；贴纸改表情、调顺序、删除。
- **GIF 收藏也能管。** 把动图存进 Telegram 的「GIF」标签页，或者从里面移除。超过 3 秒也保留完整时长；可以按需剪辑、调整尺寸、补边或裁剪，并选择帧率和画质。
- **跑在你的服务器上。** 服务端放在 VPS 上常驻，转换、上传都在服务器上做；浏览器打开服务器地址就能使用，不用安装客户端。

## 组成

| | |
|---|---|
| **服务端** `tg-stickers` | Go 写的，登录 Telegram、转换文件、提供浏览器管理面板和 API。推荐用 Docker 运行，镜像自带 ffmpeg 和网页 |
| **浏览器管理面板** | 服务端内置，打开服务器地址，输入令牌后登录 Telegram、管理贴纸包和 GIF 收藏，无需前端构建 |
| **桌面客户端（可选）** | 用 [MyGo](https://github.com/egoist/mygo) 写的原生界面，Windows、macOS、Linux 都有，连接同一个服务端使用 |

服务端和可选桌面客户端都在 GitHub Actions 上编译，从 [Releases](https://github.com/VOfficial404/tg-stickers/releases/latest) 下载。

## 开始用

### 1. 申请 api_id 和 api_hash

用要登录的账号打开 [my.telegram.org](https://my.telegram.org/apps)，进入 **API development tools**，随便填个应用名创建，记下 `api_id`（数字）和 `api_hash`（32 位）。这是 Telegram 要求第三方客户端提供的，等于这个程序的「身份证」，别给别人。

### 2. 在服务器上运行服务端

一台装了 Docker 的 Linux 服务器：

```bash
mkdir -p /opt/tg-stickers && cd /opt/tg-stickers
curl -fsSLO https://raw.githubusercontent.com/VOfficial404/tg-stickers/main/compose.yaml
docker compose up -d
docker compose exec tg-stickers tg-stickers token   # 浏览器里要填的令牌
```

已有 Docker 部署更新到带浏览器面板的镜像，在原来的 `compose.yaml` 目录执行：

```bash
docker compose pull
docker compose up -d
docker compose logs --tail=50 tg-stickers
docker compose exec tg-stickers tg-stickers token
```

继续使用原来的 `data` 挂载目录，会保留令牌、Telegram 登录会话和配置。更新后刷新网页即可，不需要另外部署静态文件。

> [!IMPORTANT]
> Compose 默认只把 8787 端口映射到服务器本机。从自己电脑访问时，先建立 SSH 隧道：
> `ssh -N -L 8787:127.0.0.1:8787 root@服务器IP`，保持运行，然后在浏览器打开 `http://127.0.0.1:8787/`。
> 有域名的话也可以用 HTTPS 反向代理（例如 Caddy 的 `reverse_proxy 127.0.0.1:8787`），浏览器打开对应的 HTTPS 地址。**别把明文端口开到公网**：拿到令牌就能操作你的 Telegram 贴纸。

### 3. 在浏览器里登录 Telegram

打开服务器地址后，先输入上一步取得的服务端令牌，再按提示依次填 api_id / api_hash、手机号（以 `+` 和国家码开头，例如美国是 `+1` 再加 10 位号码）、Telegram 发来的验证码；开了两步验证的再填密码。这里登录的是你自己的 Telegram 用户账号，不需要 Bot Token。登录后的会话保存在服务器的 `data/session.json`，之后重启也不用再登录。

**收不到验证码时：**

- 验证码通常发到你已登录的 Telegram App 里，而不是短信。Telegram 对第三方客户端（用自己 api_id 的程序）发短信有限制，见 [Telegram 的登录说明](https://core.telegram.org/api/auth)；页面只在 Telegram 确认发出后才显示「已发送」，并显示 Telegram 给出的提示。
- 「重新发送」要等服务器给出的时间到了才能点；下一次用短信、电话还是别的渠道由 Telegram 决定，页面会显示下一渠道，程序没法强制改发短信。
- 改用**扫码登录**：前提是手机上的 Telegram 已经登录这个账号。在手机号或验证码页选择扫码登录，再在手机 Telegram 的「设置 → 设备 → 连接桌面设备」扫码；二维码过期会自动换新的，开了两步验证的扫码后再填密码。随时可以取消扫码，回到手机号登录。
- 排查时看服务端日志，不需要任何验证码或密钥：`docker compose logs --tail=80 tg-stickers`。日志里不会写验证码、二维码登录地址和密码。

登录后可以在网页里查看、新建、改名和删除贴纸包，上传图片或动图制作贴纸，修改表情、调整顺序和删除贴纸；在 GIF 收藏中上传动图或移除收藏。转换和上传都由服务器完成，文件上限为每个 50 MiB。

### 云端剪辑和转换

**网页里一次选一个文件会打开可视化编辑器：**

- 时间轴显示整段动图的缩略图，拖动两端的把手选片段，点击或拖动可以看任意时刻的画面，「播放片段」按成品速度循环播放（贴纸超过 3 秒时会按实际加速倍数播放）。
- 在原画面上拖动框出要保留的区域，四角可以调整大小，选区可以锁定比例（1:1、16:9、跟随成品画布等），也能直接填 X / Y / 宽 / 高。
- 右侧实时显示成品：尺寸、时长、加速倍数，贴纸的透明部分用棋盘格表示；参数不合法（例如等比缩放后贴纸长边不足 512）时会直接提示，不能加入队列。
- 编辑器只在浏览器里解码文件来预览，真正的转换仍由服务器按这些数字完成。GIF、WebP 的逐帧预览需要 Chrome、Edge 等支持 ImageDecoder 的浏览器；其他浏览器只显示第一帧，片段可以直接填秒数。
- 一次选多个文件时不打开编辑器，按上传区的设置直接排队。桌面客户端目前仍用数字输入，暂不支持框选。

网页与桌面客户端的新建贴纸包、添加贴纸和 GIF 收藏都可以设置转换选项。转换只在服务器执行，客户端不需要安装 ffmpeg。每批上传在入队时保存选项，之后修改表单不会改变已排队的文件；新建包的第一张和后续文件也使用同一份选项。

| 选项 | 用法 |
|---|---|
| 开始 / 结束时间 | 单位为秒，可含小数。开始留空或 0 表示从头，结束留空或 0 表示到原文件结尾；结束必须晚于开始，且时段不能超出源文件时长。先选取时段，再执行视频贴纸的 3 秒加速 / 截取规则 |
| 目标宽 / 高 | 两项同时留空或 0 为自动，也可同时填写偶数尺寸。GIF 收藏可用 2–2048，自动尺寸最长 1280 且不放大小图；贴纸可用 2–512，长边必须为 512 |
| 尺寸处理 | `contain` 等比缩放到目标框内；`pad` 等比缩放并补边到精确画布（GIF 黑色，贴纸透明）；`crop` 等比放大后居中裁剪到精确画布。贴纸使用 `contain` 后若实际长边不足 512，服务端会提示改用 `pad` / `crop` |
| FPS | 留空或 0 为自动；GIF 收藏可指定 1–60，贴纸可指定 1–30。为满足 Telegram 文件大小限制，视频贴纸可能进一步降低帧率 |
| 画质 | `standard` 标准、`high` 高清、`small` 较小文件；贴纸仍需满足 Telegram 的大小限制，服务端会据此自动调整 |

**GIF 收藏没有视频贴纸的 3 秒上限**：不填时段就保留原动图完整时长，填写时段就保留该时段，转换为无声 MP4。视频贴纸始终是长边 512、最长 3 秒、最多 30 FPS、不超过 256 KB。单个源文件仍最多 50 MiB，长动图转换可能花费更多时间，反向代理需允许相应请求体和超时（见下文）。

### 可选：MyGo 桌面客户端

如果更喜欢原生窗口，从 Releases 下载对应系统的桌面客户端，填写相同的服务器地址和令牌即可。它与浏览器面板共用服务端和 Telegram 会话。

### 反向代理的上传和超时

反向代理需要允许每个 50 MiB 文件，加上 multipart 表单开销；Nginx 可给请求体设置 52 MiB。转换较大的动图和上传到 Telegram 可能需要几分钟，请同时放宽代理的读写超时。例如在 Nginx 的对应 `location /` 中配置：

```nginx
location / {
    proxy_pass http://127.0.0.1:8787;
    client_max_body_size 52m;
    proxy_read_timeout 600s;
    proxy_send_timeout 600s;
}
```

如果使用其他代理或 CDN，也要检查它的请求体上限和请求超时；代理提前断开会让浏览器报上传失败。

## 配置

都用环境变量，Docker 里写在 `compose.yaml` 的 `environment` 下：

| 变量 | 说明 |
|---|---|
| `TG_STICKERS_TOKEN` | 浏览器或桌面客户端连接用的令牌。不设时自动生成，保存在数据目录 |
| `TG_STICKERS_API_ID` / `TG_STICKERS_API_HASH` | 设了就不用在浏览器或桌面客户端里填 |
| `TG_STICKERS_PROXY` | 服务器连不上 Telegram 时用的 SOCKS5 代理，例如 `socks5://127.0.0.1:1080` |
| `TG_STICKERS_ADDR` | 监听地址，直接运行时默认 `127.0.0.1:8787` |
| `TG_STICKERS_DATA` | 数据目录，直接运行时默认是程序旁边的 `data` |
| `TG_STICKERS_FFMPEG` | ffmpeg 的路径，默认在 PATH 里找 |

不用 Docker 时，从 Releases 下载 `tg-stickers_linux_amd64` 之类的文件直接运行，服务器上要装好带 libvpx、libx264、libwebp 的 ffmpeg（Debian / Ubuntu 的 `apt install ffmpeg` 就是）。

## 先读这些

- **数据目录就是你的账号。** `data/session.json` 是 Telegram 的登录会话，拿到它就等于登录了你的账号。别泄露、别提交到 Git。不用了在网页或桌面客户端里退出 Telegram 登录，或者在 Telegram 的「设置 → 设备」里结束名为 `tg-stickers` 的会话。
- **只做普通操作。** 用的都是 Telegram 官方客户端也在用的贴纸接口，正常频率使用不会有问题；别拿它批量刷几百个贴纸包。Telegram 要求等待时，页面会提示要等多久。
- **视频贴纸的限制来自 Telegram**：WEBM（VP9）、长边 512、最长 3 秒、最多 30 帧/秒、不超过 256 KB、没有声音。普通贴纸包最多 120 张。
- **能看不能做的**：自定义表情包和面具贴纸包可以查看、调整顺序、删除，但上传只支持普通贴纸包；TGS（Lottie）动画贴纸能管理，不能从这里制作。

## 开发

```
api/          接口的数据类型和客户端（桌面客户端只依赖它）
account/      Telegram 登录、贴纸包和 GIF 收藏的操作（gotd/td）
convert/      ffmpeg 转换
server/       HTTP 接口和网页入口
cmd/          服务端入口
web/          嵌入式浏览器管理面板（静态 HTML、CSS、JavaScript，无前端构建）
app/          MyGo 桌面客户端（单独的 Go 模块）
```

编译都在 GitHub Actions 上：推到 `main` 跑 CI（含真实 ffmpeg 的转换测试）并推送 Docker 镜像；打 `v*` 标签时编译服务端和三个平台的客户端，发布到 Releases。发版前改 `app/mygo.json` 和 `app/app.go` 里的版本号，并在 `CHANGELOG.md` 写一段 `## v版本号`。
