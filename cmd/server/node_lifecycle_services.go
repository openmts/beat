package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/nodelifecycle"
)

func (services *serverServices) initializeNodeLifecycle(ctx context.Context, stores *serverStores, dataDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	external := os.Getenv("BEAT_GEOIP_MMDB_PATH")
	if external != "" && !filepath.IsAbs(external) {
		return fmt.Errorf("BEAT_GEOIP_MMDB_PATH must be absolute")
	}
	location, err := geoip.NewService(geoip.Config{Database: stores.sqlite.DB, Secrets: stores.secrets, Directory: filepath.Join(dataDir, "geoip"), ExternalMMDB: external})
	if err != nil {
		return fmt.Errorf("initialize GeoIP: %w", err)
	}
	services.geoIP = location
	if err := location.RecoverPublication(); err != nil {
		return fmt.Errorf("recover GeoIP publication: %w", err)
	}
	lifecycle, err := nodelifecycle.NewService(nodelifecycle.Config{Database: stores.sqlite.DB, Queue: services.delivery})
	if err != nil {
		return fmt.Errorf("initialize node lifecycle: %w", err)
	}
	services.nodeLifecycle = lifecycle
	stores.nodes.WithGeoIPSecrets(stores.secrets)
	services.backups.SetGeoIPAssets(location, external)
	return nil
}

func nodeLifecycleRouterOption(services *serverServices) api.RouterOption {
	return func(router *api.Router) {
		if services.geoIP != nil {
			api.WithGeoIP(services.geoIP, services.geoIP)(router)
		}
		if services.nodeLifecycle != nil {
			api.WithNodeLifecycle(services.nodeLifecycle, services.nodeLifecycle)(router)
		}
	}
}
