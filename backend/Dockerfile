FROM golang:1.21-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod ./
RUN go mod download || true

COPY . .
RUN go mod tidy

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" \
    -o trace-topo-server \
    ./cmd/server

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

ENV TZ=UTC

WORKDIR /app

COPY --from=builder /app/trace-topo-server /app/trace-topo-server

EXPOSE 4317 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/v1/health || exit 1

ENTRYPOINT ["/app/trace-topo-server"]
