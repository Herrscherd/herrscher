// Package schedule porte la règle des horaires proactifs : parser une cadence,
// dire quand la prochaine fenêtre tombe, et décider si une fenêtre ratée mérite
// encore d'être tirée. Il est pur par construction. Aucune horloge implicite,
// aucun accès disque, aucune session : l'heure arrive toujours en paramètre.
// C'est ce qui rend chaque règle testable sans daemon.
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSpec est une expression cron a 5 champs deja parsee, sous forme
// d'ensembles d'appartenance. Un tableau de bool par champ plutot qu'une liste
// d'intervalles : le test d'appartenance est le seul usage, et il devient une
// indexation.
type cronSpec struct {
	minute [60]bool
	hour   [24]bool
	dom    [32]bool // 1..31, l'indice 0 n'est jamais lu
	month  [13]bool // 1..12, l'indice 0 n'est jamais lu
	dow    [7]bool  // 0..6, dimanche = 0
	// domStar et dowStar retiennent lequel des deux champs de jour valait "*".
	// La regle Vixie en depend : deux champs restreints se lisent en OU, un seul
	// restreint se lit en ET avec le reste.
	domStar bool
	dowStar bool
}

// parseCron lit une expression a 5 champs : minute heure jour-du-mois mois
// jour-de-semaine. Chaque champ accepte "*", "N", "N-M", "*/S" et "N-M/S",
// separes par des virgules.
func parseCron(expr string) (cronSpec, error) {
	var spec cronSpec
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return spec, fmt.Errorf("cron %q: want 5 fields, got %d", expr, len(fields))
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
	for i, f := range targets {
		star, err := parseCronField(fields[i], f.set, f.min, f.max)
		if err != nil {
			return cronSpec{}, fmt.Errorf("cron %q field %d: %w", expr, i+1, err)
		}
		if f.star != nil {
			*f.star = star
		}
	}
	return spec, nil
}

// parseCronField remplit set pour un champ, et rapporte si le champ valait "*"
// nu (ce que la regle Vixie a besoin de savoir pour les deux champs de jour).
func parseCronField(field string, set []bool, min, max int) (bool, error) {
	if field == "" {
		return false, fmt.Errorf("empty")
	}
	star := field == "*"
	for _, part := range strings.Split(field, ",") {
		lo, hi, step := min, max, 1
		body := part
		if slash := strings.IndexByte(part, '/'); slash >= 0 {
			body = part[:slash]
			n, err := strconv.Atoi(part[slash+1:])
			if err != nil || n < 1 {
				return false, fmt.Errorf("bad step in %q", part)
			}
			step = n
		}
		switch {
		case body == "*":
			// lo et hi gardent les bornes du champ
		case strings.ContainsRune(body, '-'):
			ends := strings.SplitN(body, "-", 2)
			a, errA := strconv.Atoi(ends[0])
			b, errB := strconv.Atoi(ends[1])
			if errA != nil || errB != nil || a > b {
				return false, fmt.Errorf("bad range in %q", part)
			}
			lo, hi = a, b
		default:
			n, err := strconv.Atoi(body)
			if err != nil {
				return false, fmt.Errorf("bad value in %q", part)
			}
			lo, hi = n, n
		}
		if lo < min || hi > max {
			return false, fmt.Errorf("%q out of range %d-%d", part, min, max)
		}
		for v := lo; v <= hi; v += step {
			set[v] = true
		}
	}
	return star, nil
}

// matches dit si t tombe dans une minute que l'expression decrit. La seconde et
// en dessous sont ignorees : cron a la minute pour grain.
func (c cronSpec) matches(t time.Time) bool {
	if !c.minute[t.Minute()] || !c.hour[t.Hour()] || !c.month[int(t.Month())] {
		return false
	}
	dom, dow := c.dom[t.Day()], c.dow[int(t.Weekday())]
	// La regle Vixie, et elle surprend qui ne la connait pas : quand les deux
	// champs de jour sont restreints, un jour qui satisfait l'un OU l'autre
	// compte. "0 9 1 * 1" veut dire le 1er du mois et tous les lundis, pas les
	// lundis qui tombent un 1er.
	if !c.domStar && !c.dowStar {
		return dom || dow
	}
	return dom && dow
}
