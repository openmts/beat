//go:build linux

package agentoperations

import "github.com/beat/backend/internal/remoteoperations/protocol"

type fileSnapshot struct {
	starts, downloads, finalizes int
	results                      []protocol.Result
	source                       protocol.SourceInfo
}

func (server *fileServer) snapshot() fileSnapshot {
	server.mu.Lock()
	defer server.mu.Unlock()
	value := fileSnapshot{starts: server.starts, downloads: server.downloads, finalizes: server.finalizes, results: append([]protocol.Result(nil), server.results...)}
	if server.source != nil {
		value.source = *server.source
	}
	return value
}

func (server *fileServer) configure(change func(*fileServer)) {
	server.mu.Lock()
	defer server.mu.Unlock()
	change(server)
}
