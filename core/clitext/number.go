package clitext

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

const (
	thousandsGroup   = 3
	durationDecimals = 1
)

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

// FormatDuration writes d the way lang writes it: whole milliseconds below one second,
// seconds with one decimal from there on.
func FormatDuration(lang i18n.Language, d time.Duration) string {
	if d < time.Second {
		return FormatNumber(lang, float64(d.Milliseconds()), 0) + " ms"
	}
	return FormatNumber(lang, d.Seconds(), durationDecimals) + " s"
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
