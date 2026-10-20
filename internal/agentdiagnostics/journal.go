package agentdiagnostics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maximumJournalEntries = 64

// journalEntry 记录一次尝试的本地事实；它让 Agent 重启后仍能证明启动是否发生。
type journalEntry struct {
	JobID    string `json:"job_id"`
	Nonce    string `json:"nonce"`
	State    string `json:"state"`
	Sequence int64  `json:"sequence"`
	Attempt  int    `json:"attempt"`
}

const (
	journalStateLeased    = "leased"
	journalStateStarted   = "started"
	journalStateCompleted = "completed"
)

type journal struct {
	directory string
}

func openJournal(directory string) (*journal, error) {
	if err := ensurePrivateDir(directory); err != nil {
		return nil, err
	}
	return &journal{directory: directory}, nil
}

func (log *journal) path(jobID string, attempt int) string {
	return filepath.Join(log.directory, sanitizeKey(jobID)+"."+strconv.Itoa(attempt)+".json")
}

// sanitizeKey 把作业标识压缩成安全文件名；不接受路径分隔符与点号。
func sanitizeKey(jobID string) string {
	var builder strings.Builder
	for _, character := range jobID {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9', character == '-', character == '_':
			builder.WriteRune(character)
		default:
			builder.WriteRune('_')
		}
	}
	if builder.Len() == 0 || builder.Len() > 128 {
		return "invalid"
	}
	return strings.Trim(builder.String(), ".")
}

func (log *journal) save(entry journalEntry) error {
	encoded, err := json.Marshal(entry)
	if err != nil {
		return ErrStorage
	}
	if len(encoded) > 4096 {
		return ErrStorage
	}
	return writePrivateFile(log.path(entry.JobID, entry.Attempt), encoded)
}

func (log *journal) load(jobID string, attempt int) (*journalEntry, error) {
	content, err := readPrivateFile(log.path(jobID, attempt))
	if err != nil || content == nil {
		return nil, err
	}
	var entry journalEntry
	if err := json.Unmarshal(content, &entry); err != nil {
		return nil, ErrStorage
	}
	if entry.JobID != jobID || entry.Attempt != attempt || entry.State == "" {
		return nil, ErrStorage
	}
	return &entry, nil
}

func (log *journal) remove(jobID string, attempt int) error {
	return removePrivateFile(log.path(jobID, attempt))
}

// pending 返回重启后仍需裁决的尝试；只处理受限数量的条目。
func (log *journal) pending() ([]journalEntry, error) {
	entries, err := os.ReadDir(log.directory)
	if err != nil {
		return nil, ErrStorage
	}
	pending := make([]journalEntry, 0, len(entries))
	for _, directoryEntry := range entries {
		if len(pending) >= maximumJournalEntries {
			break
		}
		if directoryEntry.IsDir() || !strings.HasSuffix(directoryEntry.Name(), ".json") {
			continue
		}
		content, err := readPrivateFile(filepath.Join(log.directory, directoryEntry.Name()))
		if err != nil || content == nil {
			continue
		}
		var entry journalEntry
		if json.Unmarshal(content, &entry) != nil || entry.JobID == "" {
			continue
		}
		pending = append(pending, entry)
	}
	return pending, nil
}
