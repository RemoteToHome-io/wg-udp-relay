#!/bin/bash
# Example configuration script for WireGuard UDP Relay

# Listen address (interface and port to bind to)
LISTEN_ADDR=":51820"

# Target WireGuard server address
TARGET_ADDR="wg.example.com:51820"

# Connection timeout (how long to keep idle connections alive)
TIMEOUT="3m"

# UDP buffer size in bytes (should match MTU settings)
BUFFER_SIZE="1500"

# Run the relay with the above configuration
./wg-udp-relay \
  -listen "$LISTEN_ADDR" \
  -target "$TARGET_ADDR" \
  -timeout "$TIMEOUT" \
  -buffer "$BUFFER_SIZE"
