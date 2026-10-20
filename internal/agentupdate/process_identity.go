package agentupdate

import (
	"os"
	"strconv"
	"strings"
)

func processIdentity() (string, error) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return "", ErrManualOnly
	}
	return parseProcessIdentity(string(data), os.Getpid())
}

func parseProcessIdentity(raw string, pid int) (string, error) {
	index := strings.LastIndex(raw, ") ")
	if index < 0 || pid < 1 {
		return "", ErrHealth
	}
	fields := strings.Fields(raw[index+2:])
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return "", ErrHealth
	}
	start, err := strconv.ParseUint(fields[startTimeIndex], 10, 64)
	if err != nil || start == 0 {
		return "", ErrHealth
	}
	return strconv.Itoa(pid) + "-" + strconv.FormatUint(start, 10), nil
}
