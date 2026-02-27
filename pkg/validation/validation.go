package validation

import (
	"fmt"
	"regexp"
	"strings"
)

// kubernetesNameRegex matches valid Kubernetes resource names (RFC 1123 DNS label)
var kubernetesNameRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`)

// ValidateKubernetesName validates that a string is a valid Kubernetes resource name
func ValidateKubernetesName(name, kind string) error {
	if name == "" {
		return fmt.Errorf("%s name cannot be empty", kind)
	}
	if len(name) > 253 {
		return fmt.Errorf("%s name cannot exceed 253 characters", kind)
	}
	if !kubernetesNameRegex.MatchString(name) {
		return fmt.Errorf("invalid %s name %q: must consist of lowercase alphanumeric characters or '-', and must start and end with an alphanumeric character", kind, name)
	}
	return nil
}

// ValidateNamespace validates a Kubernetes namespace name
func ValidateNamespace(namespace string) error {
	return ValidateKubernetesName(namespace, "namespace")
}

// ValidateFilterExpression validates a tcpdump filter expression for safety.
// It rejects expressions that contain shell metacharacters that could be used
// for command injection.
func ValidateFilterExpression(filter string) error {
	if filter == "" {
		return nil // Empty filter is valid (captures all traffic)
	}

	// Reject filter expressions containing shell-dangerous characters
	dangerous := []string{";", "`", "$", "|", "&", "\n", "\r", "$(", "#{"}
	for _, d := range dangerous {
		if strings.Contains(filter, d) {
			return fmt.Errorf("invalid filter expression: contains prohibited character %q", d)
		}
	}

	// Reject unbalanced quotes that could break shell escaping
	if strings.Count(filter, "'")%2 != 0 {
		return fmt.Errorf("invalid filter expression: contains unbalanced single quotes")
	}

	return nil
}

// ValidateFilePath validates an output file path for safety
func ValidateFilePath(path string) error {
	if path == "" {
		return nil
	}

	// Reject path traversal
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid file path %q: path traversal (..) is not allowed", path)
	}

	// Reject absolute paths that write to system directories
	// Allow /var/folders/ (macOS user temp directory)
	systemDirs := []string{"/etc/", "/usr/", "/bin/", "/sbin/", "/var/", "/sys/", "/proc/"}
	for _, dir := range systemDirs {
		if strings.HasPrefix(path, dir) {
			if dir == "/var/" && strings.HasPrefix(path, "/var/folders/") {
				continue
			}
			return fmt.Errorf("invalid file path %q: cannot write to system directory", path)
		}
	}

	return nil
}
