package trafficreport

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type reportQueue struct {
	mu                       sync.Mutex
	database                 *sql.DB
	messages                 map[string]notificationqueue.Receipt
	requests                 []notificationqueue.Request
	afterMutation, readError error
	before                   func()
}

func (queue *reportQueue) Enqueue(ctx context.Context, request notificationqueue.Request, mutation func(*sql.Tx) error) (notificationqueue.Receipt, error) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if receipt, ok := queue.messages[request.SourceKey]; ok {
		receipt.Duplicate = true
		return receipt, nil
	}
	if queue.before != nil {
		queue.before()
	}
	transaction, err := queue.database.BeginTx(ctx, nil)
	if err != nil {
		return notificationqueue.Receipt{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	if err := mutation(transaction); err != nil {
		if errors.Is(err, notificationqueue.ErrSkipMutation) {
			return notificationqueue.Receipt{Skipped: true}, nil
		}
		return notificationqueue.Receipt{}, err
	}
	if queue.afterMutation != nil {
		return notificationqueue.Receipt{}, queue.afterMutation
	}
	if err := transaction.Commit(); err != nil {
		return notificationqueue.Receipt{}, err
	}
	receipt := notificationqueue.Receipt{MessageID: uuid.NewString(), State: "pending", Counts: model.NotificationDeliveryCounts{Pending: 1}}
	queue.requests = append(queue.requests, request)
	queue.messages[request.SourceKey] = receipt
	return receipt, nil
}

func (queue *reportQueue) SourceReceipt(_ context.Context, key string) (notificationqueue.Receipt, error) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.readError != nil {
		return notificationqueue.Receipt{}, queue.readError
	}
	if receipt, ok := queue.messages[key]; ok {
		return receipt, nil
	}
	return notificationqueue.Receipt{}, notificationqueue.ErrNotFound
}

type reportNodes struct {
	values []model.Node
	err    error
}

func (nodes reportNodes) ListNodes(context.Context, string) ([]model.Node, error) {
	return nodes.values, nodes.err
}

type reportMetrics struct {
	totals model.TrafficTotals
	err    error
}

func (metrics reportMetrics) QueryTrafficUsage(ctx context.Context, _ string, _, _ time.Time) (model.TrafficTotals, error) {
	if err := ctx.Err(); err != nil {
		return model.TrafficTotals{}, err
	}
	return metrics.totals, metrics.err
}

type reportFixture struct {
	database *store.SQLiteStore
	store    *store.TrafficReportScheduleStore
	service  *Service
	queue    *reportQueue
	schedule model.TrafficReportSchedule
	actor    store.EventMutationActor
	now      time.Time
}

func newReportFixture(t *testing.T) reportFixture {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	reports := store.NewTrafficReportScheduleStore(database.DB)
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	schedule := model.TrafficReportSchedule{Name: "Daily", Cadence: model.TrafficReportDaily, Timezone: "UTC", SendHour: 8, Weekday: 1, MonthDay: 1,
		AllNodes: true, AllChannels: true, Enabled: true, NextRunAt: now}
	created, err := reports.Create(t.Context(), &schedule)
	if err != nil {
		t.Fatal(err)
	}
	queue := &reportQueue{database: database.DB, messages: make(map[string]notificationqueue.Receipt)}
	service := NewService(Config{Schedules: reports, Nodes: reportNodes{values: []model.Node{{ID: "one", Name: "edge"}}},
		Metrics: reportMetrics{totals: model.TrafficTotals{Sent: 10, Received: 20}}, Queue: queue})
	service.now = func() time.Time { return now }
	actor := reportActor(t, store.NewAdminStore(database.DB))
	return reportFixture{database: database, store: reports, service: service, queue: queue, schedule: *created, now: now, actor: actor}
}

func reportActor(t *testing.T, admins *store.AdminStore) store.EventMutationActor {
	t.Helper()
	now := model.NowUTC()
	user := model.AdminUser{ID: uuid.NewString(), Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner, Enabled: true, PasswordHash: "test", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	session := model.AdminSession{ID: uuid.NewString(), UserID: user.ID, TokenHash: []byte("test"), TokenPrefix: "test", CreatedAt: now, LastActivityAt: now,
		IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour)}
	if err := admins.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	return store.EventMutationActor{UserID: user.ID, SessionID: session.ID, RequestID: "request"}
}
