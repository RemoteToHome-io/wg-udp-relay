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

# Note: Ports are dynamically exposed based on LISTEN_PORTS environment variable
# Using host network mode in docker-compose for full port access

# Run the relay
ENTRYPOINT ["/app/wg-udp-relay"]
