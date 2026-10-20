package api

import "github.com/beat/backend/internal/api/handler"

func WithAlertNotifications(notifications handler.AlertNotifications) RouterOption {
	return func(router *Router) { router.alertNotifications = notifications }
}
