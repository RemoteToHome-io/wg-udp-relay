# WireGuard UDP Relay

A lightweight, high-performance UDP relay server designed for WireGuard VPN traffic. This tool helps route WireGuard packets through intermediate servers, useful for bypassing restrictive networks or optimizing routing paths, especially from travel countries that may have poor international connection paths to your VPN server host country.

**Important security note:**

This type of relay does not have the ability to decrypt or modify the WireGuard VPN tunnel traffic (it does not have access to the client/server decryption keys).  It simply allows you to "bounce" the tunnel via a public server to assist with international routing performance or bypass endpoint restrictions.  This relay also features the ability to listen on multiple ports, so you could send your VPN client traffic on ports such as 443/UDP to make it appear more like HTTPS/3 QUIC UDP traffic, which can help bypass port restrictions and some types of throttling on a remote travel network.

**Transparency:**

The project code is AI assisted with manual review.  Given it has no access to private data or any portion of the tunnel data, the priority is quick development/enhancement and documentation.

## To-do / coming soon:
- Anycast routing - explore adding Anycast support with Vultr or DigitalOcean
- GeoDNS - Route clients to nearest relay based on location
- Load balancing - Multiple relays behind DNS round-robin

## Features

- High-performance UDP packet relaying
- Multiple port support - listen on multiple ports simultaneously
- **Automatic DDNS monitoring** - detects IP changes and updates routing
- Environment variable configuration via .env file
- Docker Compose for easy deployment
- Minimal overhead and latency
- Connection tracking and timeout management
- Graceful session migration on endpoint IP changes
- IPv4 and IPv6 support
- Host network mode for full port access

## Use Cases

- Route WireGuard traffic through intermediate servers
- Bypass network restrictions that block direct VPN connections
- Optimize routing paths for better performance
- Support multiple WireGuard clients on different ports

## Quick Start with Docker Compose

1. Clone the repository:
```bash
git clone https://github.com/RemoteToHome-io/wg-udp-relay.git
cd wg-udp-relay
```

2. Create your `.env` file from the example:
```bash
cp .env.example .env
```

3. Edit `.env` with your configuration:
```bash
# Example .env configuration
LISTEN_PORTS=51820,443
ENDPOINT_DDNS=xxxxxxx.glddns.com
ENDPOINT_PORT=58120
```

4. Start the relay:
```bash
docker-compose up -d
```

5. View logs:
```bash
docker-compose logs -f
```

6. Stop the relay:
```bash
docker-compose down
```

## Configuration

### Environment Variables (.env file)

| Variable | Description | Example | Default |
|----------|-------------|---------|---------|
| `LISTEN_PORTS` | Comma-separated list of ports to listen on | `51820,443` | Required |
| `ENDPOINT_DDNS` | Target WireGuard endpoint DDNS URL | `xxxxxxx.glddns.com` | Required |
| `ENDPOINT_PORT` | Target WireGuard endpoint port | `58120` | Required |
| `DNS_CHECK_INTERVAL` | How often to check for DNS changes | `5m`, `10m`, `1h` | `5m` |

### Docker Compose Configuration

The `docker-compose.yml` file uses `network_mode: host` to allow the container to:
- Bind to any port on the host machine
- Access the host's network interfaces directly
- Support dynamic port configuration

## WireGuard Client Configuration

To use the relay, you need to update your WireGuard client configuration to point to the relay server instead of connecting directly to your WireGuard endpoint.

### Scenario

- **Original WireGuard Server**: `xxxxxxx.glddns.com:58120`
- **Relay VPS**: `relay-vps.example.com` (or VPS IP address)
- **Relay Listen Port**: `443` (appears as HTTPS traffic to bypass restrictions)

### Configuration Change

**Before (Direct Connection):**
```ini
[Interface]
PrivateKey = <your-private-key>
Address = 10.0.0.2/24
DNS = 1.1.1.1

[Peer]
PublicKey = <server-public-key>
Endpoint = xxxxxxx.glddns.com:58120
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
```

**After (Through Relay):**
```ini
[Interface]
PrivateKey = <your-private-key>
Address = 10.0.0.2/24
DNS = 1.1.1.1

[Peer]
PublicKey = <server-public-key>
Endpoint = relay-vps.example.com:443
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
```

### What Changed?

1. **Endpoint hostname**: Changed from `xxxxxxx.glddns.com` to `relay-vps.example.com` (your relay VPS)
2. **Endpoint port**: Changed from `58120` to `443` (the port your relay is listening on)

### Why This Works

- The relay receives packets on port `443`
- The relay forwards packets to `xxxxxxx.glddns.com:58120`
- Responses are sent back through the relay to your client
- All WireGuard encryption remains end-to-end
- To firewalls and ISPs, it appears as HTTPS traffic on port `443`

### Benefits

- Bypass port restrictions (many networks allow port 443)
- Bypass protocol restrictions (appears as HTTPS)
- Maintain full WireGuard security
- Automatic endpoint IP updates via DDNS monitoring

### Important Notes

- Keep the same `PublicKey`, `PrivateKey`, and `Address` values
- Only change the `Endpoint` field
- No changes needed on the WireGuard server itself
- The relay is transparent to the WireGuard protocol

### MTU Considerations

You may need to adjust the MTU in your WireGuard client configuration for optimal performance on restricted networks:

```ini
[Interface]
PrivateKey = <your-private-key>
Address = 10.0.0.2/24
DNS = 1.1.1.1
MTU = 1384  # Lower MTU for networks with restrictions
```

**Common MTU Values:**
- `1420` - Standard WireGuard MTU (default)
- `1384` - Recommended for traveling/restricted networks
- `1280` - Minimum for IPv6 compatibility

**Important**: The relay's UDP buffer size (default 1500 bytes) does **not** need to match your client's MTU setting. The relay buffer should remain at 1500 or higher because:
- The relay receives already-encrypted WireGuard packets
- It forwards packets without re-encapsulation or decryption
- The buffer must be large enough to handle the complete packet including all headers
- Client MTU controls packet size; relay buffer controls receive capacity

## Manual Installation

### From Source

Requirements:
- Go 1.20 or later

```bash
git clone https://github.com/RemoteToHome-io/wg-udp-relay.git
cd wg-udp-relay
go build -o wg-udp-relay
```

### Command-Line Usage

```bash
# Using command-line flags
./wg-udp-relay -ports 51820,51821 -target wg.example.com:51820

# Using environment variables
export LISTEN_PORTS=51820,51821
export TARGET_ENDPOINT=wg.example.com:51820
./wg-udp-relay
```

### Command-Line Options

- `-ports <ports>` - Comma-separated list of ports to listen on (or use `LISTEN_PORTS` env var)
- `-target <address>` - Target WireGuard server address (or use `TARGET_ENDPOINT` env var)
- `-timeout <duration>` - Connection idle timeout (default: `3m`)
- `-buffer <size>` - UDP buffer size in bytes (default: `1500`, recommended to keep at 1500 or higher)
- `-dns-check <duration>` - DNS resolution check interval (or use `DNS_CHECK_INTERVAL` env var, default: `5m`)

**Note on Buffer Size**: The relay buffer size should remain at 1500 bytes or higher regardless of your WireGuard client MTU settings. The buffer must accommodate the complete encrypted packet as received from the network, while client MTU only controls the size of packets created by WireGuard. See [MTU Considerations](#mtu-considerations) for details.

## How It Works

The relay maintains a mapping of client addresses to maintain session state:

1. Relay listens on multiple configured UDP ports
2. Client sends UDP packet to any relay port
3. Relay forwards packet to the configured WireGuard endpoint
4. Server response is forwarded back to the original client
5. Sessions expire after the configured timeout period

Each listen port operates independently with its own session management.

### DDNS Monitoring

The relay automatically monitors the target endpoint's DNS record for IP changes:

1. **Initial Resolution**: On startup, the DDNS hostname is resolved to an IP address
2. **Periodic Checks**: Every `DNS_CHECK_INTERVAL` (default: 5 minutes), the relay re-resolves the hostname
3. **Change Detection**: If the IP address has changed, the relay logs the change
4. **Session Migration**: All active sessions are gracefully migrated to the new IP address
   - Old connections are closed
   - New connections are established to the new IP
   - Session state is preserved
   - No packet loss for active connections

This ensures the relay continues working even when your DDNS endpoint IP changes, which is common with dynamic DNS services.

## Architecture

```
Clients              Relay Server           WireGuard Server
-------              ------------           ----------------
Client A:51820  -->  :51820 (listening) --> endpoint:51820
Client B:51821  -->  :51821 (listening) --> endpoint:51820
Client C:51822  -->  :51822 (listening) --> endpoint:51820
```

## Performance

- Minimal CPU and memory footprint
- Handles thousands of concurrent connections per port
- Sub-millisecond forwarding latency
- Optimized buffer management
- Concurrent processing for multiple ports

## Security Considerations

- This relay does not decrypt or inspect WireGuard traffic
- All WireGuard encryption remains end-to-end
- The relay only forwards UDP packets between endpoints
- Uses host networking for optimal performance and port access
- Consider firewall rules to restrict relay access
- Store `.env` file securely and never commit it to version control

## Troubleshooting

### Container won't start
- Check that `.env` file exists and is properly configured
- Verify ports are not already in use: `sudo netstat -tulpn | grep <port>`
- Check logs: `docker-compose logs`

### Ports not accessible
- Ensure `network_mode: host` is set in docker-compose.yml
- Verify firewall rules allow UDP traffic on configured ports
- Check SELinux/AppArmor policies if applicable

### Connection issues
- Verify ENDPOINT_DDNS resolves correctly: `nslookup <domain>`
- Check that target endpoint is reachable: `nc -zvu <endpoint> <port>`
- Review relay logs for error messages
- Check for DNS change detection messages in logs

### DNS not updating
- Verify `DNS_CHECK_INTERVAL` is set appropriately in `.env`
- Check logs for DNS resolution errors
- Ensure the relay has network access to resolve DNS
- Test manual resolution: `dig +short <domain>`

## Professional Support

For professional assistance in deploying your own cloud relay for your self-hosted VPN setup, or using a managed relay through us, please contact [RemoteToHome Consulting](https://remotetohome.io/personal-support/).

We offer expert consultation and deployment services for:
- Self-hosted VPN solutions for remote work
- Custom cloud based VPN servers and relay configurations
- Custom configuration and setup reviews of GL.iNet routers for self-hosted VPNs
- High-availability VPN setups
- VPN performance optimization

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details

## Acknowledgments

Built for use with [WireGuard](https://www.wireguard.com/)

"WireGuard" is a registered trademark of Jason A. Donenfeld.
