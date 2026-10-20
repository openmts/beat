package main

import (
	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/store"
)

func metricCatalogOption(stores *serverStores) api.RouterOption {
	if stores == nil || stores.sqlite == nil {
		return api.WithMetricCatalogStore(nil)
	}
	return api.WithMetricCatalogStore(store.NewMetricCatalogStore(stores.sqlite.DB))
}

func serverMetricRetention(stores *serverStores) *metriccatalog.RetentionRunner {
	if stores == nil || stores.sqlite == nil || stores.mts == nil {
		return nil
	}
	return metriccatalog.NewRetentionRunner(store.NewMetricCatalogStore(stores.sqlite.DB), stores.mts)
}
