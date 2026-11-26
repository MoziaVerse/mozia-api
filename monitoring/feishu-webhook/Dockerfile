FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/feishu-webhook .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 app
COPY --from=builder /out/feishu-webhook /usr/local/bin/feishu-webhook
USER app
EXPOSE 8060
ENTRYPOINT ["/usr/local/bin/feishu-webhook"]
