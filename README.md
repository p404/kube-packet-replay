# kube-packet-replay

Capture and replay network packets in Kubernetes using ephemeral containers.

No sidecars, no pod restarts -- just attach, capture, and go.

## Install

```bash
go install github.com/p404/kube-packet-replay@latest
```

Or build from source:

```bash
git clone https://github.com/p404/kube-packet-replay.git
cd kube-packet-replay
make build
```

## Quick Start

**Capture TCP port 80 from a pod:**

```bash
kube-packet-replay capture "tcp port 80" pod nginx -n default
```

**Capture from all pods in a deployment for 60 seconds:**

```bash
kube-packet-replay capture "tcp port 80" deployment nginx -d 60s
```

**Replay a capture file into a pod:**

```bash
kube-packet-replay replay pod nginx -f captured.pcap.gz -n default
```

## Capture

```
kube-packet-replay capture <filter> {pod|deployment|statefulset|daemonset} <name> [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--namespace` | `-n` | `default` | Kubernetes namespace |
| `--output-file` | `-o` | `<name>-<ts>.pcap.gz` | Output file name |
| `--duration` | `-d` | Ctrl+C to stop | Capture duration (30s, 5m, 1h) |
| `--kubeconfig` | `-k` | ~/.kube/config | Path to kubeconfig |
| `--verbose` | `-v` | false | Verbose output |
| `--target-container` | | auto-detected | Target a specific container |
| `--image` | | nicolaka/netshoot:v0.13 | Debug container image |

**Filter examples:**

```bash
# By protocol
kube-packet-replay capture tcp pod nginx
kube-packet-replay capture udp pod nginx
kube-packet-replay capture icmp pod nginx

# By port
kube-packet-replay capture "tcp port 443" deployment web

# By host
kube-packet-replay capture "host 10.0.0.1" pod api-server

# Combined
kube-packet-replay capture "tcp port 80 or tcp port 443" deployment web
```

## Replay

```
kube-packet-replay replay {pod|deployment|statefulset|daemonset} <name> -f <file> [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--file` | `-f` | required | Input PCAP file |
| `--namespace` | `-n` | `default` | Kubernetes namespace |
| `--interface` | `-i` | `lo` | Network interface |
| `--speed` | `-s` | `1.0` | Speed multiplier |
| `--loop` | `-l` | `1` | Number of replay loops |
| `--image` | | nicolaka/netshoot:v0.13 | Debug container image |

```bash
# Replay at 2x speed
kube-packet-replay replay deployment nginx -f captured.pcap.gz -s 2.0

# Replay 5 times on eth0
kube-packet-replay replay pod nginx -f captured.pcap -i eth0 -l 5
```

## How It Works

1. Resolves target pods from the resource (Deployment, StatefulSet, DaemonSet)
2. Injects an ephemeral container with `tcpdump`/`tcpreplay` into each pod
3. Captures or replays traffic through the shared network namespace
4. Retrieves compressed capture files to your local machine

Ephemeral containers share the pod's network namespace, so captured traffic is exactly what the application sees.

## Requirements

- `kubectl` configured with cluster access
- Kubernetes 1.25+ (ephemeral containers enabled by default)
- Go 1.24+ (building from source only)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache License 2.0 -- see [LICENSE](LICENSE).
