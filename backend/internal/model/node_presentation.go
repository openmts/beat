package model

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxNodeTags                = 12
	MaxNodeTagLength           = 32
	MaxNodePublicRemarkLength  = 500
	MaxNodePrivateRemarkLength = 2000
)

func NormalizeNodeTags(values []string) ([]string, error) {
	if len(values) > MaxNodeTags {
		return nil, errors.New("too many node tags")
	}
	tags := make([]string, 0, len(values))
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if !validNodeTag(tag) {
			return nil, errors.New("invalid node tag")
		}
		if !containsFold(tags, tag) {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

func ValidateNodePresentation(sortOrder int, publicRemark, privateRemark string) error {
	if sortOrder < 0 {
		return errors.New("node sort order must be non-negative")
	}
	if utf8.RuneCountInString(publicRemark) > MaxNodePublicRemarkLength {
		return errors.New("public node remark is too long")
	}
	if utf8.RuneCountInString(privateRemark) > MaxNodePrivateRemarkLength {
		return errors.New("private node remark is too long")
	}
	return nil
}

func validNodeTag(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > MaxNodeTagLength || strings.Contains(value, ",") {
		return false
	}
	return strings.IndexFunc(value, unicode.IsControl) == -1
}

func containsFold(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}
