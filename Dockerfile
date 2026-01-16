# Build stage
FROM golang:1.20-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod ./

# Copy source code
COPY main.go ./

# Build the application
RUN go build -o wg-udp-relay -ldflags="-s -w" main.go

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/wg-udp-relay .

# Expose default WireGuard port
EXPOSE 51820/udp

# Run the relay
ENTRYPOINT ["/app/wg-udp-relay"]
