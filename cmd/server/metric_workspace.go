package main

import (
	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/store"
)

func metricDashboardOption(stores *serverStores) api.RouterOption {
	if stores == nil || stores.sqlite == nil {
		return api.WithMetricDashboards(nil, nil)
	}
	database := stores.sqlite.DB
	return api.WithMetricDashboards(store.NewDashboardStore(database),
		handler.NewDashboardPolicyReader(store.NewMetricCatalogStore(database), stores.nodes))
}
