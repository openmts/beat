package model

import (
	"strings"
	"testing"
)

func TestNormalizeNodeTags(t *testing.T) {
	tags, err := NormalizeNodeTags([]string{" edge ", "Core", "EDGE", "prod"})
	if err != nil {
		t.Fatalf("normalize tags: %v", err)
	}
	want := []string{"edge", "Core", "prod"}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
	for index := range want {
		if tags[index] != want[index] {
			t.Fatalf("tags[%d] = %q, want %q", index, tags[index], want[index])
		}
	}
}

func TestNormalizeNodeTagsPreservesEmptyArray(t *testing.T) {
	tags, err := NormalizeNodeTags([]string{})
	if err != nil {
		t.Fatalf("normalize empty tags: %v", err)
	}
	if tags == nil || len(tags) != 0 {
		t.Fatalf("tags = %#v, want non-nil empty slice", tags)
	}
}

func TestNormalizeNodeTagsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		tags []string
	}{
		{name: "empty", tags: []string{" "}},
		{name: "comma", tags: []string{"edge,core"}},
		{name: "too long", tags: []string{strings.Repeat("a", MaxNodeTagLength+1)}},
		{name: "too many", tags: make([]string, MaxNodeTags+1)},
	}
	for index := range tests[3].tags {
		tests[3].tags[index] = string(rune('a' + index))
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NormalizeNodeTags(test.tags); err == nil {
				t.Fatalf("NormalizeNodeTags(%v) succeeded", test.tags)
			}
		})
	}
}

func TestValidateNodePresentation(t *testing.T) {
	tests := []struct {
		name          string
		sortOrder     int
		publicRemark  string
		privateRemark string
		wantError     bool
	}{
		{name: "valid", sortOrder: 2, publicRemark: "Public context", privateRemark: "Private context"},
		{name: "negative order", sortOrder: -1, wantError: true},
		{name: "public remark too long", publicRemark: strings.Repeat("界", MaxNodePublicRemarkLength+1), wantError: true},
		{name: "private remark too long", privateRemark: strings.Repeat("界", MaxNodePrivateRemarkLength+1), wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateNodePresentation(test.sortOrder, test.publicRemark, test.privateRemark)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateNodePresentation() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}
