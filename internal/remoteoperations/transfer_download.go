package remoteoperations

import (
	"context"
	"encoding/json"
	"io"

	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/secretbox"
)

type DownloadInfo struct {
	Filename string
	Size     int64
}

type DownloadInput struct {
	ID      string
	Consume func(DownloadInfo, io.Reader) error
}

// DownloadTransfer 先提交授权审计；网络输出不占用事务或密钥锁。
func (service *Service) DownloadTransfer(ctx context.Context, actor Actor, input DownloadInput) error {
	if !service.enabled || service.spool == nil || input.Consume == nil {
		return ErrUnavailable
	}
	var item artifact
	var info DownloadInfo
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, true); err != nil {
			return err
		}
		var err error
		item, info, err = tx.admitDownload(input.ID)
		if err != nil {
			return err
		}
		return tx.audit(actor.UserID, "operation.artifact.downloaded", input.ID)
	})
	if err != nil {
		return err
	}
	reader := &downloadReader{service: service, ctx: ctx, actor: actor, id: input.ID, artifact: item}
	defer func() { _ = reader.Close() }()
	if err := input.Consume(info, reader); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (tx transaction) admitDownload(id string) (artifact, DownloadInfo, error) {
	item, err := tx.artifact(id)
	if err != nil {
		return item, DownloadInfo{}, err
	}
	if item.state != "ready" || item.direction != "collect" || item.requireScan && item.scanStatus != "clean" {
		return item, DownloadInfo{}, ErrForbidden
	}
	var sealed []byte
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT metadata_encrypted FROM operation_artifacts WHERE id=?", item.id).Scan(&sealed); err != nil {
		return item, DownloadInfo{}, ErrUnavailable
	}
	name, err := artifactFilename(tx.access, item, sealed)
	if err != nil {
		return item, DownloadInfo{}, err
	}
	return item, DownloadInfo{Filename: name, Size: item.size}, nil
}

func artifactFilename(access secretbox.ResourceAccess, item artifact, sealed []byte) (string, error) {
	plain, err := access.Open(sealed, secretbox.OperationArtifactAAD(item.id))
	if err != nil {
		return "", ErrUnavailable
	}
	defer clear(plain)
	var metadata map[string]string
	if json.Unmarshal(plain, &metadata) != nil {
		return "", ErrUnavailable
	}
	return metadata["filename"], nil
}

type downloadReader struct {
	service  *Service
	ctx      context.Context
	actor    Actor
	id       string
	artifact artifact
	index    int
	content  []byte
	offset   int
	read     int64
}

func (reader *downloadReader) Read(target []byte) (int, error) {
	if len(target) == 0 {
		return 0, nil
	}
	if reader.offset == len(reader.content) {
		clear(reader.content)
		reader.content = nil
		reader.offset = 0
		if reader.read == reader.artifact.size {
			return 0, io.EOF
		}
		if err := reader.load(); err != nil {
			return 0, err
		}
		reader.index++
	}
	count := copy(target, reader.content[reader.offset:])
	reader.offset += count
	reader.read += int64(count)
	return count, nil
}

func (reader *downloadReader) Close() error { clear(reader.content); reader.content = nil; return nil }

func (reader *downloadReader) load() error {
	var outcome chunkReadOutcome
	err := reader.service.operationMutation(reader.ctx, func(tx transaction) (*notificationqueue.Request, error) {
		if err := tx.authorize(reader.actor, false, true); err != nil {
			return nil, err
		}
		item, err := tx.artifact(reader.id)
		if err != nil {
			return nil, err
		}
		if item.id != reader.artifact.id || item.state != "ready" || item.requireScan && item.scanStatus != "clean" {
			return nil, ErrConflict
		}
		var event *notificationqueue.Request
		outcome, event, err = tx.readChunkMutation(reader.service.spool, guardedChunk{actor: reader.actor, item: item, transferID: reader.id, index: reader.index})
		return event, err
	})
	if err != nil {
		clear(outcome.content)
		return err
	}
	if outcome.corrupt {
		return ErrConflict
	}
	reader.content = outcome.content
	return nil
}
