# syntax=docker/dockerfile:1

# ---------- 构建阶段 ----------
FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct \
    CGO_ENABLED=0

# 先单独拷依赖清单，利用层缓存
COPY go.mod go.sum ./
RUN go mod download

# 编译服务二进制 + golang-migrate（mysql 驱动按 build tag 引入）
COPY . .
RUN go build -trimpath -ldflags '-s -w' -o /out/server ./cmd/server
RUN go install -tags 'mysql' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1

# ---------- 运行阶段 ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 app
WORKDIR /app
COPY --from=build /out/server ./server
COPY --from=build /go/bin/migrate ./migrate
COPY --from=build /src/migrations ./migrations
COPY --from=build /src/entrypoint.sh ./entrypoint.sh
# Windows 工作区可能带 CRLF，统一转 LF 后再赋予执行权限
RUN sed -i 's/\r$//' ./entrypoint.sh && chmod +x ./entrypoint.sh ./server ./migrate \
    && chown -R app:app /app
USER app
EXPOSE 8080
# 入口：等待 DB → migrate up（幂等）→ 起服务
ENTRYPOINT ["./entrypoint.sh"]
