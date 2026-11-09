package usage

import (
	"math"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func formatNumber(lang i18n.Language, value float64, decimals int) string {
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
		if index > 0 && (len(whole)-index)%3 == 0 {
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

const (
	moneyDecimals          = 2
	percentOneDecimalBelow = 10
)

func thousandsSeparator(lang i18n.Language) string {
	if lang == i18n.French {
		return " "
	}
	return ","
}

func formatMoney(lang i18n.Language, value float64) string {
	return "$" + formatNumber(lang, value, moneyDecimals)
}

func formatPercent(lang i18n.Language, value float64) string {
	decimals := 0
	if math.Abs(value) < percentOneDecimalBelow {
		decimals = 1
	}
	return formatNumber(lang, value, decimals) + percentSuffix
}
