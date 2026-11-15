package version

import (
	"strconv"
	"strings"
)

// Compare compares two semantic-looking version strings. Build metadata is
// ignored and a prerelease sorts before the corresponding release.
func Compare(left, right string) int {
	leftMain, leftPre := splitVersion(left)
	rightMain, rightPre := splitVersion(right)
	leftParts := strings.Split(leftMain, ".")
	rightParts := strings.Split(rightMain, ".")
	count := len(leftParts)
	if len(rightParts) > count {
		count = len(rightParts)
	}
	for index := 0; index < count; index++ {
		leftPart := "0"
		if index < len(leftParts) {
			leftPart = leftParts[index]
		}
		rightPart := "0"
		if index < len(rightParts) {
			rightPart = rightParts[index]
		}
		leftNumber, leftErr := strconv.ParseUint(leftPart, 10, 64)
		rightNumber, rightErr := strconv.ParseUint(rightPart, 10, 64)
		switch {
		case leftErr == nil && rightErr == nil:
			if leftNumber < rightNumber {
				return -1
			}
			if leftNumber > rightNumber {
				return 1
			}
		default:
			if comparison := strings.Compare(leftPart, rightPart); comparison != 0 {
				return comparison
			}
		}
	}
	if leftPre == rightPre {
		return 0
	}
	if leftPre == "" {
		return 1
	}
	if rightPre == "" {
		return -1
	}
	leftIdentifiers := strings.Split(leftPre, ".")
	rightIdentifiers := strings.Split(rightPre, ".")
	for index := 0; index < len(leftIdentifiers) && index < len(rightIdentifiers); index++ {
		leftIdentifier := leftIdentifiers[index]
		rightIdentifier := rightIdentifiers[index]
		leftNumber, leftErr := strconv.ParseUint(leftIdentifier, 10, 64)
		rightNumber, rightErr := strconv.ParseUint(rightIdentifier, 10, 64)
		switch {
		case leftErr == nil && rightErr == nil:
			if leftNumber < rightNumber {
				return -1
			}
			if leftNumber > rightNumber {
				return 1
			}
		case leftErr == nil:
			return -1
		case rightErr == nil:
			return 1
		default:
			if comparison := strings.Compare(leftIdentifier, rightIdentifier); comparison != 0 {
				return comparison
			}
		}
	}
	if len(leftIdentifiers) < len(rightIdentifiers) {
		return -1
	}
	if len(leftIdentifiers) > len(rightIdentifiers) {
		return 1
	}
	return 0
}

func splitVersion(value string) (main string, prerelease string) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	if index := strings.IndexByte(value, '+'); index >= 0 {
		value = value[:index]
	}
	if index := strings.IndexByte(value, '-'); index >= 0 {
		return value[:index], value[index+1:]
	}
	return value, ""
}
