# Performance Tuning Guide for wg-udp-relay

## Overview

By default, Linux systems have UDP receive buffers that are too small for high-throughput WireGuard relay operations. Without tuning, you may experience:
- Packet loss (1-5%)
- Poor TCP performance through the tunnel
- Throughput limited to 30-60 Mbps even on gigabit connections

With proper relay tuning, you can achieve 200-300+ Mbps throughput with zero packet loss.

**Important**: Only the relay VPS needs tuning for excellent performance. Client and server tuning are optional and typically provide minimal additional benefit.

## Relay VPS Tuning (Required for High Performance)

The relay VPS is the **only component that requires tuning** for optimal performance. Tuning the WireGuard server or client endpoints is optional and testing shows negligible performance benefit.

### Apply Tuning (Persistent)

SSH into your relay VPS and run:
```bash
# Increase UDP buffer sizes to 128 MB
sudo tee -a /etc/sysctl.conf << EOF
net.core.rmem_max=134217728
net.core.wmem_max=134217728
net.core.rmem_default=134217728
net.core.wmem_default=134217728
net.core.netdev_max_backlog=30000
EOF

# Apply immediately
sudo sysctl -p
```

### Verify Tuning
```bash
sysctl net.core.rmem_max
sysctl net.core.wmem_max
# Should show: 134217728
```

### What This Does

- **rmem_max/wmem_max**: Maximum UDP receive/send buffer sizes (128 MB)
- **rmem_default/wmem_default**: Default buffer sizes for new sockets
- **netdev_max_backlog**: Increases packet queue size for high packet rates

These settings allow the relay to buffer incoming packets during traffic bursts, preventing packet drops that destroy TCP performance.

## WireGuard Server Tuning (Optional - Minimal Benefit)

**Testing shows relay-only tuning provides excellent performance.** Server tuning is optional and provides minimal additional benefit in most scenarios.

If you still want to tune your WireGuard server (Linux VPS or system with root access):
```bash
sudo tee -a /etc/sysctl.conf << EOF
net.core.rmem_max=67108864
net.core.wmem_max=67108864
net.core.rmem_default=67108864
net.core.wmem_default=67108864
EOF

sudo sysctl -p
```

**Note:** Use 64 MB (half the relay buffer size) as the server typically handles less concurrent traffic than the relay.

## GL.iNet Router Tuning (Optional - Minimal Benefit)

**Testing shows relay-only tuning is sufficient.** Router tuning is optional and testing shows no measurable performance improvement over relay-only tuning.

If you still want to tune your GL.iNet router (or other OpenWrt device), you can apply smaller buffer sizes due to limited RAM:
```bash
# SSH into GL.iNet router
ssh root@192.168.8.1

# Add tuning to /etc/sysctl.conf
cat >> /etc/sysctl.conf << EOF
net.core.rmem_max=8388608
net.core.wmem_max=8388608
net.core.rmem_default=262144
net.core.wmem_default=262144
net.ipv4.tcp_rmem=4096 87380 8388608
net.ipv4.tcp_wmem=4096 87380 8388608
EOF

# Apply immediately
sysctl -p
```

**GL.iNet Router Considerations:**
- Buffer sizes are smaller (8 MB) due to limited RAM (256MB-1GB typical)
- Still provides significant improvement over defaults
- Persists across reboots
- Router CPU becomes bottleneck before network at ~100-200 Mbps

## Performance Expectations

### Without Tuning
- **Relay throughput**: 30-60 Mbps
- **Packet loss**: 1-5%
- **TCP retransmissions**: Hundreds per minute
- **Symptoms**: Slow downloads, video buffering, inconsistent speeds

### With Relay VPS Tuning Only (Recommended)
- **Download**: 200-300+ Mbps
- **Upload**: 150-170 Mbps
- **Packet loss**: 0%
- **TCP retransmissions**: Zero
- **Improvement**: 5-10x throughput increase over default
- **Real test results**: 301.89 Mbps down / 164.73 Mbps up

### With Full Tuning (Relay + Server + Client - Optional)
- **Performance**: Nearly identical to relay-only tuning
- **Additional benefit**: Negligible in most scenarios
- **Recommendation**: Skip unless you have specific edge cases

### Hardware Limitations
- **Linode Nanode (shared vCPU)**: ~60-80 Mbps (CPU limited)
- **Linode Dedicated 2GB (2 vCPU)**: 200-300+ Mbps
- **GL.iNet MT3000**: ~100-150 Mbps (router CPU/WireGuard crypto limited)
- **GL.iNet AXT1800**: ~150-250 Mbps (faster CPU)
- **High-end routers/VPS**: 300+ Mbps (network becomes limit)

## Testing Your Tuning

### Check for Packet Loss
```bash
# On relay VPS - check for receive buffer errors
netstat -su | grep -i "receive buffer errors"

# Run this before and during a speed test
# Errors should NOT increase if tuning is correct
```

### UDP Performance Test

Using iperf3 (install on both client and server):
```bash
# On WireGuard server (inside tunnel)
iperf3 -s

# From client (through relay tunnel)
iperf3 -c <server_tunnel_ip> -u -b 200M -t 30

# Look for:
# - 0% packet loss (or <0.1%)
# - Consistent throughput
```

### TCP Performance Test
```bash
# On WireGuard server
iperf3 -s

# From client (through relay tunnel)
iperf3 -c <server_tunnel_ip> -t 30

# Should see:
# - High sustained throughput (100+ Mbps)
# - 0 or very few retransmissions (Retr column)
# - Large congestion window (Cwnd 2-4+ MB)
```

### Real-World Test

Run a speedtest through the tunnel from a device connected to the client GL.iNet router:
- Visit https://speedtest.net
- Select a server in the same region as your WireGuard server
- Compare against direct connection benchmarks

## Troubleshooting

### Still seeing packet loss?
```bash
# Check current buffer sizes
sysctl net.core.rmem_max

# Check for buffer errors
netstat -su | grep "receive buffer errors"

# Restart relay after tuning
docker compose restart
```

### TCP still slow despite 0% UDP packet loss?

- Verify GL.iNet router tuning is applied (both client and server routers)
- Check that routers have been rebooted after tuning
- Ensure no middlebox is interfering with TCP window scaling

### Lower performance than expected?

- Check VPS CPU usage during test (`top` or `htop`)
- Verify you're using Dedicated CPU plans for high throughput
- GL.iNet routers are CPU-limited, not network-limited

## Real-World Test Results

### Test 1: Puerto Vallarta, Mexico → California Relay → Singapore Server

| Configuration | Download | Upload | Packet Loss |
|--------------|----------|--------|-------------|
| Direct connection | 90 Mbps | 65 Mbps | N/A |
| Relay (default buffers) | 65 Mbps | 60 Mbps | 1.4% |
| Relay (tuned buffers) | **332 Mbps** | **160 Mbps** | **0%** |

**Result**: 5x improvement over direct connection by optimizing the network path and eliminating packet loss through proper buffer tuning.

### Test 2: Relay-Only Tuning vs Full Stack Tuning

| Configuration | Download | Upload | Notes |
|--------------|----------|--------|-------|
| **Relay-only tuned** | **301.89 Mbps** | **164.73 Mbps** | Only relay VPS tuned |
| Full stack tuned | ~302 Mbps | ~165 Mbps | Relay + server + client tuned |

**Result**: Client and server tuning provide no measurable benefit. Relay-only tuning is sufficient for maximum performance.

## Summary

**Minimum Required Tuning:**
1. Apply UDP buffer tuning to relay VPS (10 minutes, persists forever)
2. Restart relay: `docker compose restart`

**Expected Result:**
- 5-10x throughput improvement (up to 300+ Mbps)
- Zero packet loss
- Stable, consistent performance

**Client/Server Tuning:**
- **Not recommended** - provides no measurable benefit
- Testing shows identical performance with relay-only tuning
- Save time and skip client/server tuning unless you have specific edge cases

**Key Takeaway**: The relay VPS is the only bottleneck. Tuning clients or servers does not improve performance because the relay already handles all packet buffering and forwarding efficiently. Proper relay tuning unlocks the full potential of your network path.
