# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.26.7-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN apk add --no-cache git make
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go install -ldflags="-s -w" -trimpath ./cmd/iocgo && \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -toolexec iocgo -ldflags="-s -w" -trimpath -o /out/server main.go

FROM alpine:3.24

RUN apk add --no-cache ca-certificates && \
    addgroup -S adapter && adduser -S -G adapter adapter

WORKDIR /app
COPY --from=builder /out/server ./server

RUN printf 'server:\n  port: 8080\n' > ./config.yaml && \
    chown -R adapter:adapter /app

USER adapter
EXPOSE 8080

ENTRYPOINT ["./server"]
CMD ["--port", "8080"]
