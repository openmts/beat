package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOptionalOperationsConfigurationIsDisabledByDefault(t *testing.T) {
	for _, operations := range []string{"", `,"operations":{"enabled":false}`, `,"operations":{"enabled":false,"max_concurrency":8}`} {
		path := filepath.Join(t.TempDir(), "agent.json")
		content := `{"server_url":"http://127.0.0.1:8080","agent_token":"fixture-token","node_name":"fixture","advertised_host":"127.0.0.1","ssh_port":22,"report_interval":"5s"` + operations + `}`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		config, err := LoadConfig(path)
		if operations == `,"operations":{"enabled":false,"max_concurrency":8}` {
			if err == nil {
				t.Fatal("invalid operations limits accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if config.Operations != nil && config.Operations.Enabled {
			t.Fatal("operations became enabled implicitly")
		}
	}
}
