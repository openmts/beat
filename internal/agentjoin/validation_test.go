package agentjoin

import (
	"strings"
	"testing"

	"github.com/beat/backend/internal/agentcredential"
)

func TestNormalizeServerURL(t *testing.T) {
	for _, test := range []struct {
		input, expected string
		allowHTTP       bool
	}{
		{"https://BEAT.example/", "https://beat.example", false},
		{"https://beat.example:443", "https://beat.example", false},
		{"http://localhost:8080", "http://localhost:8080", true},
		{"https://[2001:db8::1]:8443/", "https://[2001:db8::1]:8443", false},
		{"https://[::1]", "https://[::1]", false},
		{"http://127.0.0.1:80", "http://127.0.0.1", true},
	} {
		actual, err := NormalizeServerURL(test.input, test.allowHTTP)
		if err != nil || actual != test.expected {
			t.Fatalf("normalize %q = %q, %v", test.input, actual, err)
		}
	}
}

func TestNormalizeServerURLRejectsUnsafeOrigins(t *testing.T) {
	for _, value := range []string{"", "beat.example", "ftp://beat.example", "http://beat.example", "https://user:secret@beat.example", "https://beat.example/admin", "https://beat.example?", "https://beat.example#", "https://beat.example/?secret=a", "https://beat.example/#x", "https://beat.example:0", "https://beat.example:65536", "https://beat.example:", "https://beat.example:bad", "https://be at.example", "https://beat.example/\n", "https://[::1", "https://beat'$(id).example", "https://-bad.example", "https://[fe80::1%25eth0]", "https://" + strings.Repeat("a", MaximumServerURLBytes)} {
		if _, err := NormalizeServerURL(value, false); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestRequestValidation(t *testing.T) {
	token, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	base := Request{RequestID: "request_1", AgentToken: token.Plaintext, NodeName: "node-one", SSHPort: 22}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"10.0.0.10", "2001:db8::1", "node.example", "localhost"} {
		request := base
		request.AdvertisedHost = host
		if err := request.Validate(); err != nil {
			t.Fatalf("host %s: %v", host, err)
		}
	}
	for _, modify := range []func(*Request){
		func(r *Request) { r.RequestID = "" }, func(r *Request) { r.RequestID = strings.Repeat("a", MaximumRequestIDBytes+1) },
		func(r *Request) { r.RequestID = "bad id" }, func(r *Request) { r.AgentToken = "invalid" },
		func(r *Request) { r.NodeName = " " }, func(r *Request) { r.NodeName = strings.Repeat("n", MaximumNodeNameBytes+1) },
		func(r *Request) { r.NodeName = "node\nname" }, func(r *Request) { r.SSHPort = 0 }, func(r *Request) { r.SSHPort = 65536 },
		func(r *Request) { r.AdvertisedHost = "https://node.example" }, func(r *Request) { r.AdvertisedHost = strings.Repeat("h", MaximumHostBytes+1) },
	} {
		request := base
		modify(&request)
		if err := request.Validate(); err == nil {
			t.Fatalf("accepted invalid request with name %q", request.NodeName)
		}
	}
}

func TestJoinTokenGenerationAndCommand(t *testing.T) {
	token, err := GenerateTokenFrom(strings.NewReader(strings.Repeat("x", 32)))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateToken(token.Plaintext); err != nil {
		t.Fatal(err)
	}
	if len(token.Hash) != 32 || token.Prefix == token.Plaintext {
		t.Fatal("invalid credential metadata")
	}
	if _, err := GenerateTokenFrom(strings.NewReader("short")); err == nil {
		t.Fatal("expected random source error")
	}
	for _, value := range []string{"", "beat_agent_v1_wrong", TokenPrefix + "!", TokenPrefix + "eA"} {
		if err := ValidateToken(value); err == nil {
			t.Fatal("accepted malformed join token")
		}
	}
	command := JoinCommand("https://beat.example", token.Plaintext)
	if !strings.Contains(command, "sudo ./beat-agent join --server 'https://beat.example' --token '") || strings.Contains(command, "--allow-http") {
		t.Fatal(command)
	}
	if !strings.HasSuffix(JoinCommand("http://localhost", token.Plaintext), " --allow-http") {
		t.Fatal("missing HTTP opt-in")
	}
	if !strings.Contains(JoinCommand("https://beat'example", token.Plaintext), "'\\''") {
		t.Fatal("missing shell quoting")
	}
}

func TestGenerateTokenUsesIndependentRandomness(t *testing.T) {
	first, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if first.Plaintext == second.Plaintext {
		t.Fatal("generated duplicate credential")
	}
	if err := ValidateToken(first.Plaintext); err != nil {
		t.Fatal(err)
	}
}
