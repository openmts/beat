package notificationqueue

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/secretbox"
)

var (
	ErrSkipMutation     = errors.New("notification source mutation is already handled")
	ErrInvalidRequest   = errors.New("notification request is invalid")
	ErrQueueUnavailable = errors.New("notification queue is unavailable")
	ErrQueueCapacity    = errors.New("notification queue capacity reached")
	ErrNotFound         = errors.New("notification record was not found")
	ErrConflict         = errors.New("notification state changed")
	ErrForbidden        = errors.New("notification operation is not authorized")
	ErrRecentAuth       = errors.New("recent authentication is required")
	ErrRateLimited      = errors.New("notification operation budget exceeded")
	ErrLeaseLost        = errors.New("notification delivery lease is no longer current")
)

type Request struct {
	SourceKey, SourceKind, SourceID, EventKind string
	Subject, Text                              string
	Data                                       map[string]string
	ChannelIDs                                 []string
	UseSubscriptions                           bool
}

type Receipt struct {
	MessageID    string                           `json:"message_id"`
	RoutingState string                           `json:"routing_state"`
	State        string                           `json:"state"`
	Counts       model.NotificationDeliveryCounts `json:"counts"`
	Deliveries   []model.NotificationDeliveryRef  `json:"deliveries"`
	Duplicate    bool                             `json:"duplicate"`
	Skipped      bool                             `json:"skipped"`
}

// Enqueuer 在同一事务内调用业务 mutation；回调不得另开事务或发送网络请求。
type Enqueuer interface {
	Enqueue(context.Context, Request, func(*sql.Tx) error) (Receipt, error)
}

type ReceiptReader interface {
	SourceReceipt(context.Context, string) (Receipt, error)
}

type Sender interface {
	Send(context.Context, model.NotificationDispatch) model.NotificationSendResult
}

type Config struct {
	Database *sql.DB
	Secrets  secretbox.ResourceService
	Sender   Sender
	Now      func() time.Time
	Random   io.Reader
	Workers  int
	Egress   *notification.Egress
}

type Actor struct{ UserID, SessionID, RequestID, IPAddress, UserAgent string }
