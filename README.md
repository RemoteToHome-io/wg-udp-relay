# WireGuard UDP Relay

A lightweight, high-performance UDP relay server designed for WireGuard VPN traffic. This tool helps route WireGuard packets through intermediate servers, useful for bypassing restrictive networks or optimizing routing paths.

## Features

- High-performance UDP packet relaying
- Configurable listen and target addresses
- Minimal overhead and latency
- Connection tracking and timeout management
- IPv4 and IPv6 support
- Docker support for easy deployment

## Use Cases

- Route WireGuard traffic through intermediate servers
- Bypass network restrictions that block direct VPN connections
- Optimize routing paths for better performance
- Load balancing across multiple WireGuard endpoints

## Installation

### From Source

Requirements:
- Go 1.20 or later

```bash
git clone https://github.com/yourusername/wg-udp-relay.git
cd wg-udp-relay
go build -o wg-udp-relay
```

### Using Docker

```bash
docker build -t wg-udp-relay .
docker run -d -p 51820:51820/udp wg-udp-relay -listen :51820 -target your-wg-server:51820
```

## Usage

### Basic Usage

```bash
./wg-udp-relay -listen :51820 -target 203.0.113.10:51820
```

This will:
1. Listen for UDP packets on port 51820
2. Forward all packets to 203.0.113.10:51820
3. Return responses back to the original sender

### Command-Line Options

- `-listen <address>` - Address to listen on (default: `:51820`)
- `-target <address>` - Target WireGuard server address (required)
- `-timeout <duration>` - Connection idle timeout (default: `3m`)
- `-buffer <size>` - UDP buffer size in bytes (default: `1500`)

### Example Configurations

#### Simple Relay
```bash
./wg-udp-relay -listen :51820 -target wg.example.com:51820
```

#### Custom Timeout
```bash
./wg-udp-relay -listen :51820 -target wg.example.com:51820 -timeout 5m
```

#### IPv6 Support
```bash
./wg-udp-relay -listen [::]:51820 -target [2001:db8::1]:51820
```

## How It Works

The relay maintains a mapping of client addresses to maintain session state:

1. Client sends UDP packet to relay
2. Relay forwards packet to WireGuard server
3. Server response is forwarded back to original client
4. Sessions expire after the configured timeout period

## Performance

- Minimal CPU and memory footprint
- Handles thousands of concurrent connections
- Sub-millisecond forwarding latency
- Optimized buffer management

## Security Considerations

- This relay does not decrypt or inspect WireGuard traffic
- All WireGuard encryption remains end-to-end
- The relay only forwards UDP packets between endpoints
- Consider firewall rules to restrict relay access

## Limitations

- UDP only (WireGuard protocol requirement)
- No built-in authentication (use firewall rules)
- Single target server per relay instance

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details

## Acknowledgments

Built for use with [WireGuard](https://www.wireguard.com/)
