FROM golang:1.23-bookworm AS builder

WORKDIR /app

RUN apt-get update && apt-get install -y \
    gcc \
    sqlite3 \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -ldflags="-w -s" -o summarybot .

FROM alpine:latest

RUN apk --no-cache add \
    ca-certificates \
    sqlite \
    tzdata \
    wget


RUN mkdir -p /data
RUN adduser -D -u 1000 appuser
RUN mkdir -p /app && \
    chown -R appuser:appuser /app /data

WORKDIR /app/

COPY --from=builder /app/summarybot .

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

CMD ["./summarybot"]
