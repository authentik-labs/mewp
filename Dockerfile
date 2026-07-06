FROM docker.io/library/golang:1.26-trixie AS builder
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /mewp ./cmd/server

FROM docker.io/debian:trixie-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends git ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    useradd --system --uid 10001 appuser
USER appuser
COPY --from=builder /mewp /usr/local/bin/mewp
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/mewp"]
