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
