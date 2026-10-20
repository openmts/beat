package model

import "time"

type LifecycleSettings struct {
	Timezone      string `json:"timezone"`
	LeadDays      []int  `json:"lead_days"`
	PostDays      []int  `json:"post_days"`
	PublicCountry bool   `json:"public_country"`
	PublicExpiry  bool   `json:"public_expiry"`
	PublicPrice   bool   `json:"public_price"`
}

type LifecycleWrite struct {
	Provider           string `json:"provider"`
	Plan               string `json:"plan"`
	PriceMicros        *int64 `json:"price_micros,string"`
	Currency           string `json:"currency"`
	IntervalUnit       string `json:"interval_unit"`
	IntervalCount      int    `json:"interval_count"`
	ExpiresOn          string `json:"expires_on"`
	RenewalMode        string `json:"renewal_mode"`
	PrivateNotes       string `json:"private_notes"`
	ExpiryVisibility   string `json:"expiry_visibility"`
	PriceVisibility    string `json:"price_visibility"`
	ConfirmAutoAdvance bool   `json:"confirm_auto_advance"`
}

type NodeLifecycle struct {
	LifecycleWrite
	NodeID           string    `json:"node_id"`
	Status           string    `json:"status"`
	DaysRemaining    *int      `json:"days_remaining"`
	AnchorMonth      int       `json:"anchor_month"`
	AnchorDay        int       `json:"anchor_day"`
	AnchorEndOfMonth bool      `json:"anchor_end_of_month"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type LifecyclePublic struct {
	ExpiresOn     string `json:"expires_on,omitempty"`
	Status        string `json:"status,omitempty"`
	DaysRemaining *int   `json:"days_remaining,omitempty"`
	PriceMicros   *int64 `json:"price_micros,omitempty,string"`
	Currency      string `json:"currency,omitempty"`
	IntervalUnit  string `json:"interval_unit,omitempty"`
	IntervalCount int    `json:"interval_count,omitempty"`
}

type LifecycleEvent struct {
	ID               string    `json:"id"`
	NodeID           string    `json:"node_id"`
	Kind             string    `json:"kind"`
	PeriodKey        string    `json:"period_key"`
	Stage            int       `json:"stage"`
	OldExpiresOn     string    `json:"old_expires_on"`
	NewExpiresOn     string    `json:"new_expires_on"`
	SkippedIntervals int       `json:"skipped_intervals"`
	Actor            string    `json:"actor"`
	RoutingStatus    string    `json:"routing_status"`
	MessageID        string    `json:"message_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
