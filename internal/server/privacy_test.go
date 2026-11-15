package server

import (
	"encoding/json"
	"strings"
	"testing"

	"beat/internal/domain"
)

func TestNodeJSONDoesNotExposeInterfaces(t *testing.T) {
	node := domain.Node{
		Hostname: "edge-01",
		Interfaces: []domain.NetworkInterface{
			{Name: "eth0", Addresses: []string{"10.0.0.10/24"}, MAC: "52:54:00:12:34:56"},
		},
	}
	body, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{"interfaces", "10.0.0.10", "52:54:00:12:34:56"} {
		if strings.Contains(string(body), sensitive) {
			t.Fatalf("node JSON exposes sensitive interface data %q: %s", sensitive, body)
		}
	}
}
