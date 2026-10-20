package nodelifecycle

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
)

func validText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validVisibility(value string) bool {
	return value == "inherit" || value == "public" || value == "private"
}

func ValidateWrite(input model.LifecycleWrite) error {
	for _, validate := range []func(model.LifecycleWrite) error{validateMetadata, validatePrice, validateInterval, validateRenewal} {
		if err := validate(input); err != nil {
			return err
		}
	}
	return nil
}

func validateMetadata(input model.LifecycleWrite) error {
	if !validText(input.Provider, 100) || !validText(input.Plan, 100) || !validText(input.PrivateNotes, 2048) {
		return ErrInvalid
	}
	if strings.IndexFunc(input.Provider+input.Plan, unicode.IsControl) >= 0 {
		return ErrInvalid
	}
	return nil
}

func validatePrice(input model.LifecycleWrite) error {
	if input.PriceMicros != nil && *input.PriceMicros < 0 {
		return ErrInvalid
	}
	if input.Currency != "" && !validCurrency(input.Currency) {
		return ErrInvalid
	}
	if input.PriceMicros != nil && input.Currency == "" {
		return ErrInvalid
	}
	return nil
}

func validateInterval(input model.LifecycleWrite) error {
	if input.IntervalUnit != "day" && input.IntervalUnit != "month" && input.IntervalUnit != "year" {
		return ErrInvalid
	}
	if input.IntervalCount < 1 || input.IntervalCount > 120 {
		return ErrInvalid
	}
	return nil
}

func validateRenewal(input model.LifecycleWrite) error {
	if input.ExpiresOn != "" {
		if _, err := ParseDate(input.ExpiresOn); err != nil {
			return err
		}
	}
	if input.RenewalMode != "manual" && input.RenewalMode != "calendar_auto_advance" {
		return ErrInvalid
	}
	if input.RenewalMode == "calendar_auto_advance" && input.ExpiresOn == "" {
		return ErrInvalid
	}
	if !validVisibility(input.ExpiryVisibility) || !validVisibility(input.PriceVisibility) {
		return ErrInvalid
	}
	return nil
}

func validCurrency(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func DefaultSettings() model.LifecycleSettings {
	return model.LifecycleSettings{Timezone: "UTC", LeadDays: []int{0, 1, 3, 7, 14, 30}, PostDays: []int{1, 7}}
}

func ValidateSettings(input model.LifecycleSettings) error {
	if input.Timezone == "" || input.Timezone == "Local" || len(input.Timezone) > 100 {
		return ErrInvalid
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return ErrInvalid
	}
	if !validStages(input.LeadDays, false) || !validStages(input.PostDays, true) {
		return ErrInvalid
	}
	return nil
}

func validStages(stages []int, post bool) bool {
	if len(stages) > 366 || !slices.IsSorted(stages) {
		return false
	}
	for index, stage := range stages {
		if stage < 0 || stage > 365 || (post && stage == 0) || (index > 0 && stage == stages[index-1]) {
			return false
		}
	}
	return true
}
