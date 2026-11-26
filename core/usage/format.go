package usage

import (
	"math"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	percentSuffix          = " %"
	moneyDecimals          = 2
	percentOneDecimalBelow = 10
	percentSmallest        = 0.1
	moneyWholeFrom         = 100
)

func formatMoney(lang i18n.Language, value float64) string {
	return "$" + clitext.FormatNumber(lang, value, moneyDecimals)
}

func formatMoneyShort(lang i18n.Language, value float64) string {
	if math.Abs(value) >= moneyWholeFrom {
		return "$" + clitext.FormatNumber(lang, value, 0)
	}
	return formatMoney(lang, value)
}

func formatPercent(lang i18n.Language, value float64) string {
	if value > 0 && value < percentSmallest/2 {
		return "<" + clitext.FormatNumber(lang, percentSmallest, 1) + percentSuffix
	}
	decimals := 0
	if math.Abs(value) < percentOneDecimalBelow {
		decimals = 1
	}
	return clitext.FormatNumber(lang, value, decimals) + percentSuffix
}

func formatTokens(lang i18n.Language, count int64, text *renderer) string {
	value, unit := float64(count), ""
	for _, key := range []clitext.Key{clitext.SpendUnitKilo, clitext.SpendUnitMega, clitext.SpendUnitGiga} {
		if math.Round(value) < kilo {
			break
		}
		value, unit = value/kilo, text.text(key, nil)
	}
	if unit == "" {
		return clitext.FormatNumber(lang, value, 0)
	}
	return clitext.FormatNumber(lang, value, scaledDecimals(value)) + " " + unit
}

func scaledDecimals(value float64) int {
	if math.Round(value*decimalBase)/decimalBase >= wholeFrom {
		return 0
	}
	return 1
}
