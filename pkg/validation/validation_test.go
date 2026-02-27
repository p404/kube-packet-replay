package validation

import (
	"testing"
)

func TestValidateKubernetesName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		kind    string
		wantErr bool
	}{
		{"valid simple name", "nginx", "pod", false},
		{"valid with hyphens", "my-app-v2", "deployment", false},
		{"valid with numbers", "app123", "pod", false},
		{"empty name", "", "pod", true},
		{"starts with hyphen", "-nginx", "pod", true},
		{"ends with hyphen", "nginx-", "pod", true},
		{"uppercase letters", "Nginx", "pod", true},
		{"contains underscore", "my_app", "pod", true},
		{"contains dot", "my.app", "pod", true},
		{"contains space", "my app", "pod", true},
		{"single character", "a", "pod", false},
		{"single number", "1", "pod", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKubernetesName(tt.input, tt.kind)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateKubernetesName(%q, %q) error = %v, wantErr %v", tt.input, tt.kind, err, tt.wantErr)
			}
		})
	}
}

func TestValidateFilterExpression(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		wantErr bool
	}{
		{"empty filter", "", false},
		{"simple protocol", "tcp", false},
		{"port filter", "tcp port 80", false},
		{"complex filter", "tcp port 80 or tcp port 443", false},
		{"host filter", "host 10.0.0.1", false},
		{"contains semicolon", "tcp; rm -rf /", true},
		{"contains backtick", "tcp `whoami`", true},
		{"contains dollar sign", "tcp $HOME", true},
		{"contains pipe", "tcp | cat /etc/passwd", true},
		{"contains ampersand", "tcp & malicious", true},
		{"contains command sub", "tcp $(whoami)", true},
		{"contains newline", "tcp\nrm -rf /", true},
		{"unbalanced quotes", "tcp port '80", true},
		{"balanced quotes", "tcp port '80'", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFilterExpression(tt.filter)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateFilterExpression(%q) error = %v, wantErr %v", tt.filter, err, tt.wantErr)
			}
		})
	}
}

func TestValidateFilePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"empty path", "", false},
		{"simple filename", "capture.pcap", false},
		{"relative path", "output/capture.pcap", false},
		{"path traversal", "../../../etc/passwd", true},
		{"hidden path traversal", "output/../../etc/passwd", true},
		{"system etc dir", "/etc/something", true},
		{"system usr dir", "/usr/bin/something", true},
		{"system var dir", "/var/log/something", true},
		{"macos temp dir", "/var/folders/xx/yyyy/T/capture.pcap", false},
		{"home directory", "/home/user/capture.pcap", false},
		{"tmp directory", "/tmp/capture.pcap", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFilePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateFilePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestValidateNamespace(t *testing.T) {
	tests := []struct {
		name    string
		ns      string
		wantErr bool
	}{
		{"default namespace", "default", false},
		{"kube-system", "kube-system", false},
		{"custom namespace", "my-namespace", false},
		{"empty namespace", "", true},
		{"invalid namespace", "My Namespace", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.ns)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNamespace(%q) error = %v, wantErr %v", tt.ns, err, tt.wantErr)
			}
		})
	}
}
