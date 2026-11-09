package clitext

import (
	"math"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

const thousandsGroup = 3

// FormatNumber writes value with decimals digits after the separator of lang, its
// thousands grouped the way lang groups them.
func FormatNumber(lang i18n.Language, value float64, decimals int) string {
	text := strconv.FormatFloat(math.Abs(value), 'f', decimals, 64)
	whole, fraction, _ := strings.Cut(text, ".")
	var out strings.Builder
	if value < 0 {
		out.WriteByte('-')
	}
	out.WriteString(groupThousands(lang, whole))
	if decimals > 0 {
		out.WriteString(decimalSeparator(lang))
		out.WriteString(fraction)
	}
	return out.String()
}

func groupThousands(lang i18n.Language, whole string) string {
	var out strings.Builder
	for index, digit := range whole {
		if index > 0 && (len(whole)-index)%thousandsGroup == 0 {
			out.WriteString(thousandsSeparator(lang))
		}
		out.WriteRune(digit)
	}
	return out.String()
}

func decimalSeparator(lang i18n.Language) string {
	if lang == i18n.French {
		return ","
	}
	return "."
}

func thousandsSeparator(lang i18n.Language) string {
	if lang == i18n.French {
		return " "
	}
	return ","
}
