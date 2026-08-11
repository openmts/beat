package model

import "time"

type TerminalSession struct {
	ID             string
	NodeID         string
	User           string
	ConnectedAt    time.Time
	DisconnectedAt *time.Time
}
