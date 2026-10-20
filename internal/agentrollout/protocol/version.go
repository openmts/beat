package protocol

import "strings"

type releaseVersion struct {
	core []string
	pre  []string
}

func parseVersion(value string) (releaseVersion, bool) {
	var version releaseVersion
	value = strings.TrimPrefix(value, "v")
	if value == "" || len(value) > MaximumIdentifierBytes {
		return version, false
	}
	pieces := strings.Split(value, "+")
	if len(pieces) > 2 {
		return version, false
	}
	if len(pieces) == 2 && !validVersionIdentifiers(strings.Split(pieces[1], "."), false) {
		return version, false
	}
	parts := strings.SplitN(pieces[0], "-", 2)
	core, valid := versionCore(parts[0])
	if !valid {
		return version, false
	}
	version.core = core
	if len(parts) == 2 {
		version.pre = strings.Split(parts[1], ".")
		if !validVersionIdentifiers(version.pre, true) {
			return version, false
		}
	}
	return version, true
}

func validVersionIdentifiers(parts []string, prerelease bool) bool {
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, character := range part {
			if !versionCharacter(character) {
				return false
			}
		}
		if prerelease && versionNumber(part) && len(part) > 1 && part[0] == '0' {
			return false
		}
	}
	return true
}

func versionNumber(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func compareNumber(left, right string) int {
	if len(left) > len(right) {
		return 1
	}
	if len(left) < len(right) {
		return -1
	}
	return strings.Compare(left, right)
}

func CompareVersions(target, current string) (int, bool) {
	left, ok := parseVersion(target)
	if !ok {
		return 0, false
	}
	right, ok := parseVersion(current)
	if !ok {
		return 0, false
	}
	for index := range left.core {
		if order := compareNumber(left.core[index], right.core[index]); order != 0 {
			return order, true
		}
	}
	return comparePrerelease(left.pre, right.pre), true
}

func comparePrerelease(left, right []string) int {
	if len(left) == 0 {
		if len(right) == 0 {
			return 0
		}
		return 1
	}
	if len(right) == 0 {
		return -1
	}
	for index := 0; index < len(left) && index < len(right); index++ {
		if left[index] != right[index] {
			return compareVersionLabel(left[index], right[index])
		}
	}
	return compareLength(len(left), len(right))
}

func compareVersionLabel(left, right string) int {
	numericLeft, numericRight := versionNumber(left), versionNumber(right)
	if numericLeft && numericRight {
		return compareNumber(left, right)
	}
	if numericLeft {
		return -1
	}
	if numericRight {
		return 1
	}
	return strings.Compare(left, right)
}

func compareLength(left, right int) int {
	if left > right {
		return 1
	}
	if left < right {
		return -1
	}
	return 0
}

func versionCore(value string) ([]string, bool) {
	core := strings.Split(value, ".")
	if len(core) != 3 {
		return nil, false
	}
	for _, number := range core {
		if !canonicalNumber(number) {
			return nil, false
		}
	}
	return core, true
}

func canonicalNumber(value string) bool {
	return versionNumber(value) && (len(value) == 1 || value[0] != '0')
}

// ReleaseTag 仅从已验证的manifest版本导出CI tag，绝不接受独立上传的身份规则。
func ReleaseTag(version string) (string, error) {
	if _, valid := parseVersion(version); !valid {
		return "", ErrManifest
	}
	return "v" + strings.TrimPrefix(version, "v"), nil
}

func versionCharacter(character rune) bool {
	return character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character == '-'
}

// SameReleaseVersion 只忽略可选的v前缀，仍区分prerelease和build标识。
func SameReleaseVersion(left, right string) bool {
	if _, valid := parseVersion(left); !valid {
		return false
	}
	if _, valid := parseVersion(right); !valid {
		return false
	}
	return strings.TrimPrefix(left, "v") == strings.TrimPrefix(right, "v")
}
