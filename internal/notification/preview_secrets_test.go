package notification

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPreviewRedactsLiteralBodyValuesAndRetainsMessageSubstitution(t *testing.T) {
	canonical, err := CanonicalizeProvider(ConfigWrite{ChannelType: TypeCustomHTTP, Enabled: true,
		Config: json.RawMessage(`{"endpoint":"https://receiver.example","body":{"api_key":"PRIVATE_BODY_SECRET","value":"prefix {{text}}","number":12345678,"items":[true,"{{secret.key}}"]},"named_secrets":{"key":"PRIVATE_NAMED_SECRET"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewDispatch(testDispatch(TypeCustomHTTP, canonical.Secret))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(preview.Body, "PRIVATE_") || strings.Contains(preview.Body, "12345678") || !strings.Contains(preview.Body, "Message") || !json.Valid([]byte(preview.Body)) {
		t.Fatal("request preview exposed a stored literal credential or lost safe substitutions")
	}
}
