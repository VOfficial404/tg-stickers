## 下载哪个文件

| 设备 | 下载 | 说明 |
|---|---|---|
| Windows 电脑 | `tg-stickers-desktop-setup-*-amd64.exe` | 安装包。不想安装就下 `*-windows-amd64-portable.zip`，解压即用；ARM 电脑选带 `arm64` 的 |
| Mac | `tg-stickers-desktop-*.dmg` | Intel 和 Apple 芯片通用 |
| Linux 桌面 | `.deb`（Debian、Ubuntu） | 其他发行版下 `.tar.gz` |
| 服务器 | 推荐用 Docker：`ghcr.io/VOfficial404/tg-stickers` | 不用 Docker 时下 `tg-stickers_<系统>_<架构>`，并自己装好 ffmpeg |

**桌面客户端尚未做代码签名**：Windows 弹出 SmartScreen 时点「更多信息 → 仍要运行」；macOS 第一次打开时在 Finder 里右键 → 打开。

**校验**：`checksums.txt` 里是所有文件的 SHA-256。
