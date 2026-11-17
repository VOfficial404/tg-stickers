FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY account ./account
COPY convert ./convert
COPY server ./server
COPY web ./web
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/tg-stickers ./cmd/tg-stickers

FROM alpine:3.22
# 转换要用到的编码器：视频贴纸 VP9、GIF 收藏 H.264、静态贴纸 WEBP。缺了任何一个就让构建失败
RUN apk add --no-cache ffmpeg ca-certificates \
    && encoders="$(ffmpeg -hide_banner -encoders)" \
    && for e in libvpx-vp9 libx264 libwebp; do echo "$encoders" | grep -q " $e " || { echo "ffmpeg 缺少编码器 $e"; exit 1; }; done
COPY --from=build /out/tg-stickers /usr/local/bin/tg-stickers
# 容器里监听所有地址；对外开放到哪里由端口映射决定（compose.yaml 默认只映射到服务器本机）
ENV TG_STICKERS_DATA=/data \
    TG_STICKERS_ADDR=0.0.0.0:8787
VOLUME /data
EXPOSE 8787
ENTRYPOINT ["tg-stickers"]
CMD ["serve"]
