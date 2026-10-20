package agentenrollment

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func validMetadata() Metadata {
	return Metadata{Hostname: "node-1", AdvertisedHost: "192.0.2.10", OS: "linux", Architecture: "amd64",
		AgentVersion: "0.2.0-dev", Virtualization: "kvm", SSHPort: 22, InstallationNonce: "nonce-1"}
}

func TestMetadataValidateAcceptsRepresentativeReport(t *testing.T) {
	if err := validMetadata().Validate(); err != nil {
		t.Fatalf("metadata = %v", err)
	}
	blank := Metadata{InstallationNonce: "n", SSHPort: MinSSHPort}
	if err := blank.Validate(); err != nil {
		t.Fatalf("empty display fields = %v", err)
	}
	maximum := validMetadata()
	maximum.SSHPort = MaxSSHPort
	maximum.Hostname = strings.Repeat("h", MaxHostnameLength)
	if err := maximum.Validate(); err != nil {
		t.Fatalf("boundary metadata = %v", err)
	}
}

func TestMetadataValidateRejectsOutOfRangeFields(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(metadata *Metadata)
		expected string
	}{
		{name: "hostname too long", expected: "hostname must be at most",
			mutate: func(metadata *Metadata) { metadata.Hostname = strings.Repeat("h", MaxHostnameLength+1) }},
		{name: "advertised host too long", expected: "advertised host must be at most",
			mutate: func(metadata *Metadata) { metadata.AdvertisedHost = strings.Repeat("h", MaxAdvertisedHostLength+1) }},
		{name: "os too long", expected: "os must be at most",
			mutate: func(metadata *Metadata) { metadata.OS = strings.Repeat("o", MaxOSLength+1) }},
		{name: "architecture too long", expected: "architecture must be at most",
			mutate: func(metadata *Metadata) { metadata.Architecture = strings.Repeat("a", MaxArchitectureLength+1) }},
		{name: "agent version too long", expected: "agent version must be at most",
			mutate: func(metadata *Metadata) { metadata.AgentVersion = strings.Repeat("v", MaxAgentVersionLength+1) }},
		{name: "virtualization too long", expected: "virtualization must be at most",
			mutate: func(metadata *Metadata) { metadata.Virtualization = strings.Repeat("z", MaxVirtualizationLength+1) }},
		{name: "control characters", expected: "contains control characters",
			mutate: func(metadata *Metadata) { metadata.Hostname = "node\u0000" }},
		{name: "invalid utf8", expected: "is not valid UTF-8",
			mutate: func(metadata *Metadata) { metadata.OS = string([]byte{0xff, 0xfe}) }},
		{name: "empty nonce", expected: "installation nonce must be 1-",
			mutate: func(metadata *Metadata) { metadata.InstallationNonce = "" }},
		{name: "nonce too long", expected: "installation nonce must be 1-",
			mutate: func(metadata *Metadata) {
				metadata.InstallationNonce = strings.Repeat("n", MaxInstallationNonceBytes+1)
			}},
		{name: "nonce control", expected: "installation nonce contains control characters",
			mutate: func(metadata *Metadata) { metadata.InstallationNonce = "nonce\n" }},
		{name: "port too low", expected: "SSH port must be",
			mutate: func(metadata *Metadata) { metadata.SSHPort = MinSSHPort - 1 }},
		{name: "port too high", expected: "SSH port must be",
			mutate: func(metadata *Metadata) { metadata.SSHPort = MaxSSHPort + 1 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			metadata := validMetadata()
			test.mutate(&metadata)
			err := metadata.Validate()
			if !errors.Is(err, ErrInvalidMetadata) || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("validate = %v", err)
			}
		})
	}
}

func TestMetadataCanonicalBindsEveryField(t *testing.T) {
	base := validMetadata()
	canonical := base.Canonical()
	if !bytes.HasPrefix(canonical, field([]byte(metadataDomain))) {
		t.Fatalf("missing metadata domain: %x", canonical)
	}
	if digest := MetadataDigest(base); digest != Digest(canonical) {
		t.Fatalf("digest = %x", digest)
	}
	mutations := []func(metadata *Metadata){
		func(metadata *Metadata) { metadata.Hostname = "node-2" },
		func(metadata *Metadata) { metadata.AdvertisedHost = "192.0.2.11" },
		func(metadata *Metadata) { metadata.OS = "freebsd" },
		func(metadata *Metadata) { metadata.Architecture = "arm64" },
		func(metadata *Metadata) { metadata.AgentVersion = "0.2.1" },
		func(metadata *Metadata) { metadata.Virtualization = "none" },
		func(metadata *Metadata) { metadata.SSHPort = 2222 },
		func(metadata *Metadata) { metadata.InstallationNonce = "nonce-2" },
	}
	for index, mutate := range mutations {
		changed := base
		mutate(&changed)
		if bytes.Equal(changed.Canonical(), canonical) || MetadataDigest(changed) == MetadataDigest(base) {
			t.Fatalf("mutation %d left the canonical metadata unchanged", index)
		}
	}
}
