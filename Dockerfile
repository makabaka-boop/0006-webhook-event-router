# syntax=docker/dockerfile:1

# ---- 构建阶段：多架构编译 ----
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/router ./cmd/server

# ---- 运行阶段：最小镜像 ----
FROM --platform=$TARGETPLATFORM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/router /app/router

# 数据目录（运行时挂载卷）
RUN mkdir -p /data && chown 65532:65532 /data

ENV DB_PATH=/data/router.db \
    HTTP_ADDR=:8080

EXPOSE 8080

USER 65532:65532
VOLUME ["/data"]

ENTRYPOINT ["/app/router"]
