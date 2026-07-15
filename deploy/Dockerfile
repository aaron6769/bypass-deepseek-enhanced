# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /app
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go install -ldflags="-s -w" -trimpath ./cmd/iocgo
RUN mkdir -p /out \
    && CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
       go build -toolexec /go/bin/iocgo -ldflags="-s -w" -trimpath -o /out/server ./main.go \
    && printf 'server:\n  port: 8080\n' > /out/config.yaml

FROM alpine:3.19.0

WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder --chmod=755 /out/server ./server
COPY --from=builder /out/config.yaml ./config.yaml

EXPOSE 8080
ENV ARG="--port 8080"
CMD ["sh", "-c", "exec ./server ${ARG}"]
