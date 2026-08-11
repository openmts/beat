package api

import (
	"context"
	"io"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/store"
)

type optionTerminal struct{}

type optionTrafficReports struct{}

type optionMaintenance struct{}

func (optionMaintenance) Overview(context.Context) (model.MaintenanceOverview, error) {
	return model.MaintenanceOverview{}, nil
}

func (optionMaintenance) UpdateSettings(
	_ context.Context,
	settings model.MaintenanceSettings,
) (model.MaintenanceSettings, error) {
	return settings, nil
}

func (optionMaintenance) StartManual() error { return nil }

func (optionTrafficReports) List(context.Context) ([]model.TrafficReportSchedule, error) {
	return nil, nil
}

func (optionTrafficReports) Create(
	context.Context,
	*model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	return nil, nil
}

func (optionTrafficReports) Update(
	context.Context,
	string,
	*model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	return nil, nil
}

func (optionTrafficReports) Delete(context.Context, string) error { return nil }

func (optionTrafficReports) TestRun(
	context.Context,
	string,
) (model.TrafficReportRunResult, error) {
	return model.TrafficReportRunResult{}, nil
}

func (optionTerminal) OpenTerminal(context.Context, string, io.ReadWriteCloser) error { return nil }

func (optionTerminal) ExecuteBatch(context.Context, []string, string) []service.BatchResult {
	return nil
}

func TestRouterOptions(t *testing.T) {
	router := &Router{}
	terminal := optionTerminal{}
	delivery := notification.NewService()
	reports := optionTrafficReports{}
	maintenanceService := optionMaintenance{}
	settings := &store.SiteSettingsStore{}
	WithAuthTokens("admin", "agent")(router)
	WithTerminalOperations(terminal)(router)
	WithNotificationService(delivery)(router)
	WithTrafficReportService(reports)(router)
	WithSiteSettingsStore(settings)(router)
	WithMaintenanceService(maintenanceService)(router)
	if !router.adminAuthEnabled || !router.agentAuthEnabled || router.adminToken != "admin" ||
		router.legacyAgentToken != "agent" || router.terminal == nil || router.delivery != delivery ||
		router.trafficReports == nil || router.siteSettingsStore != settings || router.maintenance == nil {
		t.Fatalf("router = %#v", router)
	}
}

func TestRouterAuthOptionsAreIndependent(t *testing.T) {
	router := &Router{}
	WithAdminToken("admin")(router)
	if !router.adminAuthEnabled || router.agentAuthEnabled {
		t.Fatalf("admin option auth flags = %#v", router)
	}
	WithAgentAuthentication("")(router)
	if !router.adminAuthEnabled || !router.agentAuthEnabled || router.legacyAgentToken != "" {
		t.Fatalf("agent option auth flags = %#v", router)
	}
}
