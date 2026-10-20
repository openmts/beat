package nodelifecycle

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func validWrite() model.LifecycleWrite {
	return model.LifecycleWrite{Provider: "Provider", Plan: "Plan", IntervalUnit: "month", IntervalCount: 1, RenewalMode: "manual", ExpiryVisibility: "inherit", PriceVisibility: "inherit"}
}

func TestLifecycleValidationAndPriceStringDTO(t *testing.T) {
	value := validWrite()
	price := int64(math.MaxInt64)
	value.PriceMicros = &price
	value.Currency = "USD"
	if err := ValidateWrite(value); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil || !strings.Contains(string(encoded), `"price_micros":"9223372036854775807"`) {
		t.Fatal(string(encoded), err)
	}
	var decoded model.LifecycleWrite
	if err = json.Unmarshal(encoded, &decoded); err != nil || *decoded.PriceMicros != price {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(`{"price_micros":9223372036854775807}`), &decoded); err == nil {
		t.Fatal("numeric DTO accepted")
	}
	edits := []func(*model.LifecycleWrite){
		func(v *model.LifecycleWrite) { v.Provider = strings.Repeat("x", 101) }, func(v *model.LifecycleWrite) { v.Plan = "bad\nplan" }, func(v *model.LifecycleWrite) { v.PrivateNotes = strings.Repeat("x", 2049) },
		func(v *model.LifecycleWrite) { negative := int64(-1); v.PriceMicros = &negative }, func(v *model.LifecycleWrite) { v.Currency = "usd" }, func(v *model.LifecycleWrite) { v.Currency = "US" }, func(v *model.LifecycleWrite) { v.Currency = "" },
		func(v *model.LifecycleWrite) { v.IntervalUnit = "week" }, func(v *model.LifecycleWrite) { v.IntervalCount = 121 }, func(v *model.LifecycleWrite) { v.ExpiresOn = "bad" }, func(v *model.LifecycleWrite) { v.RenewalMode = "charge" },
		func(v *model.LifecycleWrite) { v.RenewalMode = "calendar_auto_advance" }, func(v *model.LifecycleWrite) { v.ExpiryVisibility = "bad" }, func(v *model.LifecycleWrite) { v.PriceVisibility = "bad" },
	}
	for _, edit := range edits {
		invalid := value
		edit(&invalid)
		if err = ValidateWrite(invalid); err != ErrInvalid {
			t.Fatal(invalid, err)
		}
	}
	value.RenewalMode = "calendar_auto_advance"
	value.ExpiresOn = "2024-01-31"
	value.ConfirmAutoAdvance = true
	if err = ValidateWrite(value); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleSettingsValidation(t *testing.T) {
	settings := DefaultSettings()
	if err := ValidateSettings(settings); err != nil || settings.PublicCountry || settings.PublicExpiry || settings.PublicPrice {
		t.Fatal(settings, err)
	}
	for _, zone := range []string{"", "Local", "bad/zone", strings.Repeat("x", 101)} {
		invalid := settings
		invalid.Timezone = zone
		if err := ValidateSettings(invalid); err != ErrInvalid {
			t.Fatal(zone, err)
		}
	}
	for _, stages := range [][]int{{-1}, {366}, {1, 1}, {2, 1}} {
		invalid := settings
		invalid.LeadDays = stages
		if err := ValidateSettings(invalid); err != ErrInvalid {
			t.Fatal(stages, err)
		}
	}
	settings.PostDays = []int{0}
	if err := ValidateSettings(settings); err != ErrInvalid {
		t.Fatal(err)
	}
	settings.PostDays = []int{}
	settings.LeadDays = []int{}
	if err := ValidateSettings(settings); err != nil {
		t.Fatal(err)
	}
}
