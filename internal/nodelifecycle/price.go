package nodelifecycle

import (
	"math"
	"strconv"
	"strings"
)

const PriceScale int64 = 1000000

// ParsePrice 把十进制主单位转换为micros，不经过浮点数。
func ParsePrice(value string) (int64, error) {
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || !decimalDigits(parts[0]) {
		return 0, ErrInvalid
	}
	integer, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || integer > math.MaxInt64/PriceScale {
		return 0, ErrInvalid
	}
	fraction, err := parseFraction(parts)
	if err != nil {
		return 0, err
	}

	result := integer * PriceScale
	if result > math.MaxInt64-fraction {
		return 0, ErrInvalid
	}
	return result + fraction, nil
}

func decimalDigits(value string) bool {
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func FormatPrice(micros int64) (string, error) {
	if micros < 0 {
		return "", ErrInvalid
	}
	integer := strconv.FormatInt(micros/PriceScale, 10)
	fraction := micros % PriceScale
	if fraction == 0 {
		return integer, nil
	}
	padded := strconv.FormatInt(PriceScale+fraction, 10)[1:]
	return integer + "." + strings.TrimRight(padded, "0"), nil
}

func parseFraction(parts []string) (int64, error) {
	if len(parts) == 1 {
		return 0, nil
	}
	if parts[1] == "" || len(parts[1]) > 6 || !decimalDigits(parts[1]) {
		return 0, ErrInvalid
	}
	fraction, err := strconv.ParseInt(parts[1]+strings.Repeat("0", 6-len(parts[1])), 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	return fraction, nil
}
