# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Input validation for all CLI parameters (namespace, resource names, filter expressions, file paths)
- `--image` flag to override the default debug container image
- `version` subcommand to display build information
- `pkg/validation` package with comprehensive input sanitization
- GitHub Actions CI/CD workflows (build, test, lint, release)
- golangci-lint configuration
- goreleaser configuration for automated releases
- Comprehensive test suite for input validation
- Security policy (SECURITY.md)
- Contributing guidelines (CONTRIBUTING.md)
- Code of Conduct
- Issue and PR templates

### Fixed
- Command injection vulnerabilities in shell command construction
- Double-counting bug in multi-pod replay statistics
- Channel panic when capture goroutines outlive the parent function
- Broken ephemeral container cleanup (ephemeral containers cannot be deleted)
- Error variable shadowing in Kubernetes client initialization
- Signal handler leak (missing signal.Stop cleanup)
- Unsafe global os.Stdout manipulation in concurrent goroutines

### Changed
- Pinned debug container image to `nicolaka/netshoot:v0.13` (was `:latest`)
- Pinned Dockerfile base images to specific versions
- Extended `Writer` interface with `Colorize`, `FormatBold`, `FormatHighlight` methods
- Replaced all unsafe type assertions to `*ConsoleWriter` with interface method calls
- Replaced error message parsing for container discovery with proper Kubernetes API calls
- Removed direct `fmt.Printf` calls from Kubernetes resource discovery in favor of structured output
- Consolidated duplicate `formatBytes` implementations into a single function
- Improved error messages with more context

### Removed
- Unused `pkg/cli` package (duplicated `pkg/output` functionality)

## [0.1.0] - 2024-01-01

### Added
- Initial release
- Packet capture from individual pods using ephemeral containers
- Packet replay to pods using tcpreplay
- Multi-pod capture and replay for Deployments, StatefulSets, and DaemonSets
- Automatic resource type detection
- tcpdump filter expression support
- Compressed capture file handling (.pcap.gz)
- Color-coded terminal output with progress tracking
- Cross-platform builds (Linux, macOS, Windows)
- Docker support
- Makefile with comprehensive build targets
