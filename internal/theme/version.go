package theme

import "strings"

func parseVersion(version string) [3]int {
	core := version
	if index := strings.IndexAny(core, "+-"); index >= 0 {
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	var result [3]int
	for index := 0; index < len(parts) && index < 3; index++ {
		for _, character := range parts[index] {
			if character < '0' || character > '9' {
				break
			}
			result[index] = result[index]*10 + int(character-'0')
		}
	}
	return result
}

func compareVersions(left, right [3]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
