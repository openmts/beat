package remoteoperations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

func (service *Service) inspectArtifact(ctx context.Context, scan artifactScan) error {
	reader := &scanReader{service: service, ctx: ctx, scan: scan}
	defer reader.clear()
	hash := sha256.New()
	size, err := io.Copy(hash, reader)
	if err != nil {
		return err
	}
	if size != scan.item.size || hex.EncodeToString(hash.Sum(nil)) != scan.item.digest {
		return errArtifactCorrupt
	}
	if service.spool.scanner != nil {
		reader.clear()
		reader.index = 0
		reader.read = 0
		if err := service.spool.scanner.Scan(ctx, reader); err != nil {
			return errScanRejected
		}
		if reader.read != scan.item.size {
			return errScanRejected
		}
	}
	return nil
}

func (service *Service) scanStatus() string {
	if service.spool.scanner != nil {
		return "clean"
	}
	return "unavailable"
}

type scanReader struct {
	service       *Service
	ctx           context.Context
	scan          artifactScan
	content       []byte
	offset, index int
	read          int64
}

func (reader *scanReader) clear() { clear(reader.content); reader.content = nil; reader.offset = 0 }

func (reader *scanReader) Read(target []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if len(target) == 0 {
		return 0, nil
	}
	if reader.offset == len(reader.content) {
		reader.clear()
		if reader.read == reader.scan.item.size {
			return 0, io.EOF
		}
		if err := reader.service.write(reader.ctx, func(tx transaction) error {
			if err := tx.checkScan(reader.scan); err != nil {
				return err
			}
			content, err := tx.readChunk(reader.service.spool, reader.scan.item, reader.index)
			if err != nil {
				return errArtifactCorrupt
			}
			reader.content = content
			return nil
		}); err != nil {
			return 0, err
		}
		reader.index++
	}
	count := copy(target, reader.content[reader.offset:])
	reader.offset += count
	reader.read += int64(count)
	return count, nil
}

func (service *Service) trackScan(id string, cancel context.CancelFunc) (func(), error) {
	service.scanMu.Lock()
	defer service.scanMu.Unlock()
	if service.closed {
		return nil, ErrUnavailable
	}
	service.scans[id] = cancel
	service.scanWG.Add(1)
	return func() {
		cancel()
		service.scanMu.Lock()
		delete(service.scans, id)
		service.scanMu.Unlock()
		service.scanWG.Done()
	}, nil
}

func (service *Service) stopScan(id string) {
	service.scanMu.Lock()
	defer service.scanMu.Unlock()
	if cancel := service.scans[id]; cancel != nil {
		cancel()
	}
}

func (service *Service) Close() {
	service.scanMu.Lock()
	service.closed = true
	for _, cancel := range service.scans {
		cancel()
	}
	service.scanMu.Unlock()
	service.scanWG.Wait()
}
