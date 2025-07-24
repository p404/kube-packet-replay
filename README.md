# kube-packet-replay

A powerful Kubernetes network packet capture and replay tool that simplifies debugging network issues in containerized environments.

## Features

- **Easy Packet Capture**: Capture network packets from any Kubernetes pod without modifying deployments
- **Multi-Pod Support**: Capture from entire Deployments, StatefulSets, or DaemonSets simultaneously
- **Smart Replay**: Replay captured packets to different pods or environments
- **Protocol Filtering**: Use tcpdump filters to capture specific protocols or traffic patterns
- **Non-Intrusive**: Uses ephemeral debug containers - no changes to your running workloads
- **Cross-Platform**: Works on Linux, macOS, and Windows

## Installation

### Using Homebrew (macOS/Linux)
```bash
brew tap p404/kube-packet-replay
brew install kube-packet-replay
```

### Using Go
```bash
go install github.com/p404/kube-packet-replay@latest
```

### From Source
```bash
git clone https://github.com/p404/kube-packet-replay.git
cd kube-packet-replay
make install
```

## Quick Start

### Capture packets from a pod
```bash
# Capture all TCP traffic from a specific pod
kube-packet-replay capture "tcp" pod nginx-7d9b8c5f9-abc123

# Capture HTTP traffic (port 80) from all pods in a deployment
kube-packet-replay capture "tcp port 80" deployment nginx

# Capture with specific duration
kube-packet-replay capture "udp" pod dns-server --duration=30s
```

### Replay captured packets
```bash
# Replay to a different pod
kube-packet-replay replay capture-file.pcap.gz pod test-pod

# Replay to all pods in a deployment
kube-packet-replay replay capture-file.pcap.gz deployment app-deployment

# Replay with speed control
kube-packet-replay replay capture-file.pcap.gz pod target-pod --speed=2.0
```

## Common Use Cases

### 1. Debug intermittent connection issues
```bash
# Capture failed connections
kube-packet-replay capture "tcp[tcpflags] & (tcp-rst) != 0" deployment frontend --duration=5m
```

### 2. Analyze API communication
```bash
# Capture traffic between services
kube-packet-replay capture "host api-service and port 8080" pod frontend-pod
```

### 3. Debug DNS resolution
```bash
# Capture DNS queries
kube-packet-replay capture "udp port 53" deployment app --duration=1m
```

### 4. Test load balancing
```bash
# Capture from all backend pods
kube-packet-replay capture "tcp" deployment backend -o backend-traffic.pcap.gz
```

## Advanced Usage

### Filter Examples

| Filter | Description |
|--------|-------------|
| `tcp` | All TCP traffic |
| `udp port 53` | DNS queries |
| `tcp port 80 or tcp port 443` | HTTP/HTTPS traffic |
| `host 10.0.0.1` | Traffic to/from specific IP |
| `net 10.0.0.0/16` | Traffic within subnet |
| `tcp[tcpflags] & (tcp-syn) != 0` | TCP SYN packets |

### Command Options

#### Capture Options
- `--namespace, -n`: Kubernetes namespace (default: "default")
- `--output-file, -o`: Output filename (default: auto-generated)
- `--duration, -d`: Capture duration (e.g., "30s", "5m")
- `--target-container`: Specific container in multi-container pods
- `--verbose, -v`: Enable verbose output

#### Replay Options
- `--namespace, -n`: Target namespace
- `--interface, -i`: Network interface (default: auto-detect)
- `--speed, -s`: Replay speed multiplier
- `--loop, -l`: Loop the replay
- `--verbose, -v`: Enable verbose output

## Architecture

kube-packet-replay uses Kubernetes ephemeral containers to inject a network debugging container into your pods. This approach:
- Requires no changes to your application
- Shares the pod's network namespace
- Automatically cleans up after capture/replay
- Works with any container runtime

## Requirements

- Kubernetes 1.23+ (ephemeral containers support)
- kubectl configured with cluster access
- Permissions to create ephemeral containers

## Building from Source

```bash
# Clone the repository
git clone https://github.com/p404/kube-packet-replay.git
cd kube-packet-replay

# Build for current platform
make build

# Build for all platforms
make build-all

# Run tests
make test

# Build Docker image
make docker-build
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Built with [cobra](https://github.com/spf13/cobra) for CLI
- Uses [client-go](https://github.com/kubernetes/client-go) for Kubernetes integration
- Inspired by the need for better network debugging in Kubernetes