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
)

func formatMoney(lang i18n.Language, value float64) string {
	return "$" + clitext.FormatNumber(lang, value, moneyDecimals)
}

func formatPercent(lang i18n.Language, value float64) string {
	decimals := 0
	if math.Abs(value) < percentOneDecimalBelow {
		decimals = 1
	}
	return clitext.FormatNumber(lang, value, decimals) + percentSuffix
}
