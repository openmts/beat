package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/store"
)

// registerTrafficCalibrationRoutes 只注册 owner 专属的校准读写；全部要求最近认证。
func (router *Router) registerTrafficCalibrationRoutes(mux *http.ServeMux) {
	if router.trafficCalibration == nil || router.nodeStore == nil {
		return
	}
	h := handler.NewTrafficCalibrationHandler(router.nodeStore, router.mtsStore, router.trafficCalibration)
	owner := func(fn http.HandlerFunc) http.Handler { return router.metadataOwner(fn) }
	mux.Handle("GET /api/v1/admin/nodes/{id}/traffic-calibrations", owner(h.HandleHistory))
	mux.Handle("POST /api/v1/admin/nodes/{id}/traffic-calibrations", owner(h.HandleCreate))
	mux.Handle("POST /api/v1/admin/nodes/{id}/traffic-calibrations/{calibration_id}/revoke", owner(h.HandleRevoke))
	mux.Handle("POST /api/v1/admin/nodes/{id}/traffic-calibrations/{calibration_id}/replace", owner(h.HandleReplace))
}

// trafficAccountingResolver 在注入账本时构造校准感知的周期结算解析器。
func (router *Router) trafficAccountingResolver() *store.TrafficAccountingResolver {
	if router.trafficCalibration == nil {
		return nil
	}
	return store.NewTrafficAccountingResolver(router.mtsStore, router.trafficCalibration)
}
