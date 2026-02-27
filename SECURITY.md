# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 0.1.x   | Yes                |

As the project matures, this table will be updated to reflect which versions receive security fixes.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

If you discover a security vulnerability in kube-packet-replay, please report it responsibly by sending an email to:

**security@kube-packet-replay.dev**

Include the following in your report:

- A description of the vulnerability
- Steps to reproduce the issue
- The potential impact
- Any suggested remediation, if applicable

### Response timeline

- **Acknowledgment** -- We will acknowledge receipt of your report within 48 hours.
- **Initial assessment** -- We will provide an initial assessment of the report within 5 business days.
- **Resolution** -- We aim to release a fix within 30 days of confirming the vulnerability, depending on severity and complexity.

We will coordinate with you on disclosure timing. We ask that you do not publicly disclose the vulnerability until a fix has been released.

## Security Considerations

kube-packet-replay interacts directly with Kubernetes cluster resources and network traffic. Users and operators should be aware of the following:

### NET_ADMIN capability

Packet capture and replay require the `NET_ADMIN` Linux capability. The ephemeral debug containers created by this tool are granted this capability. Ensure that your cluster's admission policies (e.g., Pod Security Standards, OPA Gatekeeper, Kyverno) allow ephemeral containers with `NET_ADMIN` in the namespaces where you intend to use this tool.

### Ephemeral containers

Ephemeral containers share the network namespace of the target pod. This means the debug container has full visibility into all network traffic of the pod. Limit access to this tool to users who are authorized to inspect network traffic in the target namespace.

### Debug image

This tool uses the [`nicolaka/netshoot`](https://github.com/nicolaka/netshoot) container image by default. This image contains a broad set of network diagnostic tools. Consider the following:

- **Image provenance** -- Verify that the image is pulled from a trusted registry. In air-gapped or restricted environments, mirror the image to an internal registry and update the image reference accordingly.
- **Image pinning** -- For production or sensitive environments, pin the image to a specific digest rather than using the `latest` tag.

### Captured data

PCAP capture files may contain sensitive information, including application payloads, authentication tokens, and personally identifiable information (PII). Handle capture files with the same level of care as production logs. Store and transmit them securely, and delete them when they are no longer needed.

### RBAC

Using this tool requires Kubernetes RBAC permissions to create ephemeral containers, exec into pods, and read resource metadata (Deployments, StatefulSets, DaemonSets). Grant these permissions only to users and service accounts that require them.
