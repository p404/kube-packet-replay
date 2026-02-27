# Contributing to kube-packet-replay

Thank you for your interest in contributing to kube-packet-replay. This document provides guidelines and instructions for contributing.

## Table of Contents

- [Development Environment Setup](#development-environment-setup)
- [Code Style](#code-style)
- [Running Tests](#running-tests)
- [Submitting Changes](#submitting-changes)
- [Reporting Issues](#reporting-issues)
- [Code of Conduct](#code-of-conduct)

## Development Environment Setup

### Prerequisites

- Go 1.24 or later
- kubectl configured with access to a Kubernetes cluster
- A Kubernetes cluster with ephemeral containers enabled (Kubernetes 1.25+)
- GNU Make

### Getting started

1. Fork the repository on GitHub.

2. Clone your fork:

   ```bash
   git clone https://github.com/<your-username>/kube-packet-replay.git
   cd kube-packet-replay
   ```

3. Install development tools (golangci-lint, goimports):

   ```bash
   make dev-setup
   ```

4. Verify the build and run checks:

   ```bash
   make dev
   ```

   This will format the code, run `go vet`, and build the binary.

5. Run the full check suite:

   ```bash
   make check
   ```

   This runs formatting, vetting, linting, and tests in sequence.

## Code Style

This project follows standard Go conventions.

- **Formatting** -- All code must be formatted with `gofmt`. Run `make fmt` to format the entire codebase.
- **Linting** -- Code is checked with [golangci-lint](https://golangci-lint.run/). Run `make lint` to check for issues.
- **Imports** -- Use `goimports` to organize import statements. It is installed by `make dev-setup`.
- **Naming** -- Follow the [Go naming conventions](https://go.dev/doc/effective_go#names). Use clear, descriptive names. Exported identifiers should have doc comments.
- **Error handling** -- Always handle errors explicitly. Do not discard errors with `_` unless there is a documented reason.
- **Comments** -- Exported types, functions, and packages should have doc comments. Use complete sentences.

## Running Tests

Run all tests:

```bash
make test
```

Run tests with the race detector enabled:

```bash
make test-race
```

Run tests with a coverage report:

```bash
make test-coverage
```

This generates `coverage.html` in the project root, which you can open in a browser to inspect line-by-line coverage.

## Submitting Changes

### Pull request process

1. **Create a branch** from `main` for your work:

   ```bash
   git checkout -b feature/my-change
   ```

2. **Make your changes.** Keep commits focused and atomic. Write clear commit messages that explain _why_ the change was made, not just what was changed.

3. **Run checks locally** before pushing:

   ```bash
   make check
   ```

4. **Push your branch** to your fork:

   ```bash
   git push origin feature/my-change
   ```

5. **Open a pull request** against the `main` branch of the upstream repository.

### Pull request guidelines

- Keep PRs small and focused on a single concern when possible.
- Include a clear description of what the PR does and why.
- Reference any related issues using `Fixes #123` or `Relates to #123`.
- Ensure all CI checks pass before requesting review.
- Be responsive to review feedback.

### Commit messages

Write commit messages in the imperative mood (e.g., "Add support for..." rather than "Added support for..."). A good commit message structure:

```
Short summary of the change (72 chars or less)

Longer explanation of the change if needed. Wrap at 72 characters.
Explain the problem that this commit solves and why this approach
was chosen.
```

## Reporting Issues

When reporting a bug, please include:

- **kube-packet-replay version** (output of `kube-packet-replay --version` or the Git commit hash)
- **Kubernetes version** (output of `kubectl version`)
- **Go version** (output of `go version`), if building from source
- **Operating system and architecture**
- **Steps to reproduce** the issue
- **Expected behavior** versus **actual behavior**
- **Relevant logs or output** (use code blocks for formatting)

For feature requests, describe the use case and the expected behavior. Explain why the feature would be useful for the project.

