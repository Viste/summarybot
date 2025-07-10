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

FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    ca-certificates \
    sqlite3 \
    wget \
    tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

RUN mkdir -p /data /app

COPY --from=builder /app/summarybot /app/summarybot

WORKDIR /app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

CMD ["./summarybot"]
