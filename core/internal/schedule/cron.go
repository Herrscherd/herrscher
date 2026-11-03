// Package schedule porte la règle des horaires proactifs : parser une cadence,
// dire quand la prochaine fenêtre tombe, et décider si une fenêtre ratée mérite
// encore d'être tirée. Il est pur par construction. Aucune horloge implicite,
// aucun accès disque, aucune session : l'heure arrive toujours en paramètre.
// C'est ce qui rend chaque règle testable sans daemon.
package schedule

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const cronFieldCount = 5

var errCronSyntax = errors.New("invalid cron expression")

type cronSpec struct {
	minute  [60]bool
	hour    [24]bool
	dom     [32]bool
	month   [13]bool
	dow     [7]bool
	domStar bool
	dowStar bool
}

func parseCron(expr string) (cronSpec, error) {
	var spec cronSpec
	fields := strings.Fields(expr)
	if len(fields) != cronFieldCount {
		return spec, fmt.Errorf("%w %q: want %d fields, got %d", errCronSyntax, expr, cronFieldCount, len(fields))
	}
	targets := []struct {
		set      []bool
		min, max int
		star     *bool
	}{
		{spec.minute[:], 0, 59, nil},
		{spec.hour[:], 0, 23, nil},
		{spec.dom[:], 1, 31, &spec.domStar},
		{spec.month[:], 1, 12, nil},
		{spec.dow[:], 0, 6, &spec.dowStar},
	}
	for idx, field := range targets {
		star, err := parseCronField(fields[idx], field.set, field.min, field.max)
		if err != nil {
			return cronSpec{}, fmt.Errorf("cron %q field %d: %w", expr, idx+1, err)
		}
		if field.star != nil {
			*field.star = star
		}
	}
	return spec, nil
}

func parseCronField(field string, set []bool, low, high int) (bool, error) {
	if field == "" {
		return false, fmt.Errorf("%w: empty field", errCronSyntax)
	}
	star := strings.HasPrefix(field, "*")
	for _, part := range strings.Split(field, ",") {
		if err := setCronPart(part, set, low, high); err != nil {
			return false, err
		}
	}
	return star, nil
}

func setCronPart(part string, set []bool, low, high int) error {
	body, step, err := splitCronStep(part)
	if err != nil {
		return err
	}
	lowest, highest, err := cronBounds(body, part, step, low, high)
	if err != nil {
		return err
	}
	if lowest < low || highest > high {
		return fmt.Errorf("%w: %q out of range %d-%d", errCronSyntax, part, low, high)
	}
	for v := lowest; v <= highest; v += step {
		set[v] = true
	}
	return nil
}

func splitCronStep(part string) (string, int, error) {
	slash := strings.IndexByte(part, '/')
	if slash < 0 {
		return part, 1, nil
	}
	stepVal, err := strconv.Atoi(part[slash+1:])
	if err != nil || stepVal < 1 {
		return "", 0, fmt.Errorf("%w: bad step in %q", errCronSyntax, part)
	}
	return part[:slash], stepVal, nil
}

func cronBounds(body, part string, step, low, high int) (int, int, error) {
	switch {
	case body == "*":
		return low, high, nil
	case strings.ContainsRune(body, '-'):
		lowerPart, upperPart, _ := strings.Cut(body, "-")
		fromVal, errA := strconv.Atoi(lowerPart)
		toVal, errB := strconv.Atoi(upperPart)
		if errA != nil || errB != nil || fromVal > toVal {
			return 0, 0, fmt.Errorf("%w: bad range in %q", errCronSyntax, part)
		}
		return fromVal, toVal, nil
	}
	value, err := strconv.Atoi(body)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: bad value in %q", errCronSyntax, part)
	}
	if step != 1 {
		return 0, 0, fmt.Errorf("%w: step needs a range or a star in %q", errCronSyntax, part)
	}
	return value, value, nil
}

func (c cronSpec) matches(instant time.Time) bool {
	if !c.minute[instant.Minute()] || !c.hour[instant.Hour()] || !c.month[int(instant.Month())] {
		return false
	}
	dom, dow := c.dom[instant.Day()], c.dow[int(instant.Weekday())]
	if !c.domStar && !c.dowStar {
		return dom || dow
	}
	return dom && dow
}
