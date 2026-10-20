package remoteoperations

import "context"

type Status struct {
	Enabled           bool  `json:"enabled"`
	SpoolConfigured   bool  `json:"spool_configured"`
	ScannerConfigured bool  `json:"scanner_configured"`
	MaximumFileBytes  int64 `json:"maximum_file_bytes"`
	ChunkBytes        int   `json:"chunk_bytes"`
}

func (service *Service) Status(ctx context.Context, actor Actor) (Status, error) {
	err := service.atomicWrite(ctx, func(tx transaction) error { return tx.authorize(actor, false, false) })
	if err != nil {
		return Status{}, err
	}
	status := Status{Enabled: service.enabled, SpoolConfigured: service.spool != nil, MaximumFileBytes: MaximumFileBytes, ChunkBytes: SpoolChunkBytes}
	if service.spool != nil {
		status.ScannerConfigured = service.spool.scanner != nil
	}
	return status, nil
}
