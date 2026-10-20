package runtimediag

import (
	"errors"
	"io"
	"os"
	"time"
)

// boundedWriter 把 profile 输出限制在单件上限内，超限即失败并阻止继续写入。
type boundedWriter struct {
	target    io.Writer
	remaining int64
	written   int64
	exceeded  bool
}

func (writer *boundedWriter) Write(payload []byte) (int, error) {
	if int64(len(payload)) > writer.remaining {
		writer.exceeded = true
		return 0, Error{Code: CodeSizeExceeded}
	}
	written, err := writer.target.Write(payload)
	writer.written += int64(written)
	writer.remaining -= int64(written)
	return written, err
}

// consumeReader 只在完整读取后原子消费产物；中途断开时保留文件供重试。
type consumeReader struct {
	file     *os.File
	expected int64
	read     int64
	consume  func()
	release  func()
}

func (reader *consumeReader) Read(payload []byte) (int, error) {
	read, err := reader.file.Read(payload)
	reader.read += int64(read)
	return read, err
}

func (reader *consumeReader) Close() error {
	err := reader.file.Close()
	if reader.read >= reader.expected {
		reader.consume()
	} else {
		reader.release()
	}
	return err
}

// Download 返回已完成产物的只读流；同一产物只能成功消费一次。
func (service *Service) Download(ownerID, id string) (io.ReadCloser, Capture, error) {
	service.mu.Lock()
	item, err := service.ownedLocked(ownerID, id)
	if err != nil {
		service.mu.Unlock()
		return nil, Capture{}, err
	}
	if item.Status != StatusComplete || item.consuming {
		service.mu.Unlock()
		return nil, Capture{}, Error{Code: CodeConflict}
	}
	file, openErr := os.Open(item.path)
	if openErr != nil {
		service.mu.Unlock()
		return nil, Capture{}, Error{Code: CodeNotFound}
	}
	item.consuming = true
	metadata := item.Capture
	service.mu.Unlock()
	return &consumeReader{file: file, expected: metadata.Bytes,
		consume: func() { service.consume(id) }, release: func() { service.release(id) }}, metadata, nil
}

// release 在下载中断时复位消费标记，让所有者可以重新下载同一产物。
func (service *Service) release(id string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if item, ok := service.items[id]; ok {
		item.consuming = false
	}
}

func (service *Service) consume(id string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	item, ok := service.items[id]
	if !ok {
		return
	}
	service.removeFile(item)
	delete(service.items, id)
	service.observe(Event{Kind: item.Kind, Result: EventDownloaded, Bytes: item.Bytes})
}

// Cancel 停止运行中的捕获并删除产物，同时保留一条 cancelled 元数据供界面显示。
func (service *Service) Cancel(ownerID, id string) error {
	item, err := service.claimForCancel(ownerID, id)
	if err != nil {
		return err
	}
	if item != nil {
		item.cancel()
		<-item.done
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if item == nil {
		return nil
	}
	service.removeFile(item)
	if item.Status == StatusRunning {
		item.Status, item.FailureCode, item.Bytes = StatusCancelled, "", 0
	}
	return nil
}

func (service *Service) claimForCancel(ownerID, id string) (*entry, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	item, err := service.ownedLocked(ownerID, id)
	if err != nil {
		return nil, err
	}
	if item.Status == StatusRunning {
		return item, nil
	}
	service.removeFile(item)
	if item.Status != StatusComplete {
		return nil, nil
	}
	item.Status, item.Bytes = StatusCancelled, 0
	service.observe(Event{Kind: item.Kind, Result: EventCancelled})
	return nil, nil
}

func (service *Service) ownedLocked(ownerID, id string) (*entry, error) {
	service.sweepLocked(service.now())
	item, ok := service.items[id]
	if !ok || item.OwnerID != ownerID {
		return nil, Error{Code: CodeNotFound}
	}
	return item, nil
}

// RevokeSession 取消某个会话（登出或撤销）拥有的全部捕获。
func (service *Service) RevokeSession(sessionID string) {
	for _, item := range service.sessionItems(sessionID) {
		if item.Status == StatusRunning {
			item.cancel()
			<-item.done
		}
		service.mu.Lock()
		service.removeFile(item)
		delete(service.items, item.ID)
		service.mu.Unlock()
	}
}

func (service *Service) sessionItems(sessionID string) []*entry {
	service.mu.Lock()
	defer service.mu.Unlock()
	items := make([]*entry, 0, 1)
	for _, item := range service.items {
		if item.SessionID == sessionID {
			items = append(items, item)
		}
	}
	return items
}

// Sweep 清理过期产物并返回删除数量；运行中超时的捕获会被取消。
func (service *Service) Sweep() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.sweepLocked(service.now())
}

func (service *Service) sweepLocked(now time.Time) int {
	removed := 0
	for id, item := range service.items {
		if item.Status == StatusRunning {
			if now.After(item.Deadline.Add(service.limits.MaximumDuration)) {
				item.cancel()
			}
			continue
		}
		if !now.After(item.ExpiresAt) {
			continue
		}
		expired := item.Status == StatusComplete
		if expired {
			item.Status = StatusExpired
		}
		service.removeFile(item)
		delete(service.items, id)
		removed++
		if expired {
			service.observe(Event{Kind: item.Kind, Result: EventExpired})
		}
	}
	return removed
}

// Close 取消全部捕获、等待写入协程退出并删除私有目录中的所有产物。
func (service *Service) Close() error {
	service.mu.Lock()
	service.closed = true
	for _, item := range service.items {
		if item.Status == StatusRunning {
			item.cancel()
		}
	}
	service.mu.Unlock()
	service.wait.Wait()
	service.mu.Lock()
	defer service.mu.Unlock()
	var result error
	for id, item := range service.items {
		if err := os.Remove(item.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			service.cleanupFailures++
			result = errors.Join(result, err)
		}
		delete(service.items, id)
	}
	return result
}
