package schedule

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	errInvalidSchedule = errors.New("invalid schedule")
	errNoAnchor        = errors.New("schedule has no anchor to count from")
	errCronNoWindow    = errors.New("cron has no window")
)

// DefaultGrace est le retard maximum qu'une fenetre ratee peut avoir et etre
// quand meme tiree au demarrage du daemon. Un seul chiffre, court et
// previsible, plutot qu'une regle derivee de la periode : derivee, elle ferait
// toujours tirer un horaire quotidien et jamais un horaire aux cinq minutes.
// Un digest de 9h recu a 9h45 sert encore, le meme a 18h derange.
const DefaultGrace = time.Hour

const minGrace = time.Minute

const maxCronLookahead = 366 * 24 * time.Hour

// Schedule est un horaire persiste. Session et Agent sont deux cibles
// mutuellement exclusives, Every et Cron deux cadences mutuellement exclusives.
type Schedule struct {
	Name    string `json:"name"`
	Session string `json:"session,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Project string `json:"project,omitempty"`
	Task    string `json:"task"`
	Every   string `json:"every,omitempty"`
	Cron    string `json:"cron,omitempty"`
	Grace   string `json:"grace,omitempty"`
	Paused  bool   `json:"paused,omitempty"`
	// LastRun est le point d'ou la prochaine fenetre se compte, en RFC3339 : le
	// dernier tir qui a effectivement atteint une session, ou l'instant ou une
	// fenetre perimee a ete abandonnee (voir Stale). Vide veut dire jamais tire,
	// et l'ancre est alors CreatedAt.
	LastRun   string `json:"lastRun,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// SessionName est le nom de la session qu'un horaire a cible agent possede. Il
// derive du nom de l'horaire, donc il est stable d'un tick au suivant : c'est
// ce qui permet de retrouver la session plutot que d'en creer une par tir.
func SessionName(s Schedule) string { return "schedule-" + s.Name }

// Validate porte toutes les gardes de la creation. Elles tombent ici, une fois,
// plutot qu'au tir : un horaire impossible doit etre refuse a la face de qui
// l'ecrit, pas echouer en silence chaque minute. now est le point d'ou la
// recherche de fenetre part, et il arrive en parametre comme partout ailleurs
// dans ce paquet : la garde du cron impossible se teste alors a une date fixe.
func Validate(sched Schedule, now time.Time) error {
	if strings.TrimSpace(sched.Name) == "" {
		return fmt.Errorf("%w: needs a name", errInvalidSchedule)
	}
	if strings.TrimSpace(sched.Task) == "" {
		return fmt.Errorf("%w: needs a task", errInvalidSchedule)
	}
	switch {
	case sched.Session == "" && sched.Agent == "":
		return fmt.Errorf("%w: needs a target: --session or --agent", errInvalidSchedule)
	case sched.Session != "" && sched.Agent != "":
		return fmt.Errorf("%w: takes one target: --session or --agent, not both", errInvalidSchedule)
	}
	if sched.Project != "" && sched.Agent == "" {
		return fmt.Errorf("%w: --project only means something with --agent, which opens the session", errInvalidSchedule)
	}
	switch {
	case sched.Every == "" && sched.Cron == "":
		return fmt.Errorf("%w: needs a cadence: --every or --cron", errInvalidSchedule)
	case sched.Every != "" && sched.Cron != "":
		return fmt.Errorf("%w: takes one cadence: --every or --cron, not both", errInvalidSchedule)
	}
	if _, err := graceOf(sched); err != nil {
		return err
	}
	if sched.Every != "" {
		_, err := period(sched)
		return err
	}
	if _, err := parseCron(sched.Cron); err != nil {
		return err
	}
	_, err := Next(sched, now)
	return err
}

func period(sched Schedule) (time.Duration, error) {
	span, err := time.ParseDuration(sched.Every)
	if err != nil {
		return 0, fmt.Errorf("--every %q: %w", sched.Every, err)
	}
	if span <= 0 {
		return 0, fmt.Errorf("%w: --every %q must be positive", errInvalidSchedule, sched.Every)
	}
	return span, nil
}

func graceOf(sched Schedule) (time.Duration, error) {
	if sched.Grace == "" {
		return DefaultGrace, nil
	}
	span, err := time.ParseDuration(sched.Grace)
	if err != nil {
		return 0, fmt.Errorf("--grace %q: %w", sched.Grace, err)
	}
	if span < minGrace {
		return 0, fmt.Errorf("%w: --grace %q must be at least %s", errInvalidSchedule, sched.Grace, minGrace)
	}
	return span, nil
}

func anchor(s Schedule) (time.Time, bool) {
	for _, stamp := range []string{s.LastRun, s.CreatedAt} {
		if stamp == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, stamp); err == nil {
			return t.Local(), true
		}
	}
	return time.Time{}, false
}

// Next rend la premiere fenetre strictement apres `after`.
func Next(sched Schedule, after time.Time) (time.Time, error) {
	if sched.Every != "" {
		span, err := period(sched)
		if err != nil {
			return time.Time{}, err
		}
		return after.Add(span), nil
	}
	spec, err := parseCron(sched.Cron)
	if err != nil {
		return time.Time{}, err
	}
	next := after.Truncate(time.Minute).Add(time.Minute)
	deadline := after.Add(maxCronLookahead)
	for ; next.Before(deadline); next = next.Add(time.Minute) {
		if spec.matches(next) {
			return next, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: %q within %s", errCronNoWindow, sched.Cron, maxCronLookahead)
}

// NextWindow rend la prochaine fenetre a venir, comptee depuis l'ancre et non
// depuis l'instant ou la question est posee. La difference se voit sur --every :
// un horaire de 24h ancre a 9h ouvre sa prochaine fenetre demain a 9h, et non
// dans vingt-quatre heures a compter de maintenant. C'est ce que veut savoir qui
// lit `schedule list`.
//
// Un horaire deja du rend une fenetre passee, puisque c'est celle qu'il attend :
// a l'appelant de poser Due d'abord s'il veut distinguer les deux.
func NextWindow(sched Schedule) (time.Time, error) {
	anchorAt, ok := anchor(sched)
	if !ok {
		return time.Time{}, errNoAnchor
	}
	return Next(sched, anchorAt)
}

// Window rend la fenetre que la boucle est en train de servir : la plus recente
// a `now` ou avant. C'est elle, et non l'instant de la livraison, que LastRun
// doit retenir.
//
// La boucle a le grain de la minute et le tir lui-meme prend du temps, donc
// compter la fenetre suivante depuis la livraison arrondirait chaque periode au
// tick d'apres. La perte n'est pas ponctuelle, elle s'accumule : un horaire aux
// trente minutes tire 46 fois par jour au lieu de 48, et
// glisse de trois quarts d'heure en vingt-quatre heures.
func Window(sched Schedule, now time.Time) (time.Time, bool) {
	grace, err := graceOf(sched)
	if err != nil {
		return time.Time{}, false
	}
	return lastWindow(sched, now, grace)
}

// Due dit si la prochaine fenetre est arrivee. C'est la question que la boucle
// pose a chaque tick.
func Due(sched Schedule, now time.Time) bool {
	anchorAt, ok := anchor(sched)
	if !ok {
		return false
	}
	next, err := Next(sched, anchorAt)
	return err == nil && !next.After(now)
}

// Stale dit qu'une fenetre est arrivee mais qu'elle est trop vieille pour etre
// servie. Elle ne doit ni tirer ni rester en attente : la boucle realigne alors
// l'horaire sur maintenant. Sans ce troisieme cas, une fenetre que CatchUp
// refuse resterait due, et le tick suivant la tirerait quand meme, sans dire son
// retard : le digest de 9h serait rendu a 18h en se presentant comme celui du
// matin, ce que la grace existe precisement pour empecher.
func Stale(sched Schedule, now time.Time) bool {
	if sched.Paused || !Due(sched, now) {
		return false
	}
	fire, _ := CatchUp(sched, now)
	return !fire
}

// CatchUp decide du sort d'une fenetre ratee pendant que le daemon etait
// arrete. Il tire au plus une fois, sur la fenetre ratee la plus recente, et
// seulement si son retard tient dans le delai de grace. Rejouer chaque fenetre
// ratee facturerait N tours qui raisonnent tous sur un monde disparu.
func CatchUp(sched Schedule, now time.Time) (bool, time.Duration) {
	if sched.Paused {
		return false, 0
	}
	grace, err := graceOf(sched)
	if err != nil {
		return false, 0
	}
	window, ok := lastWindow(sched, now, grace)
	if !ok {
		return false, 0
	}
	late := now.Sub(window)
	if late > grace {
		return false, 0
	}
	return true, late
}

func lastWindow(sched Schedule, now time.Time, grace time.Duration) (time.Time, bool) {
	anchorAt, ok := anchor(sched)
	if !ok {
		return time.Time{}, false
	}
	if sched.Every != "" {
		span, err := period(sched)
		if err != nil {
			return time.Time{}, false
		}
		elapsed := now.Sub(anchorAt)
		if elapsed < span {
			return time.Time{}, false
		}
		return anchorAt.Add(elapsed.Truncate(span)), true
	}
	spec, err := parseCron(sched.Cron)
	if err != nil {
		return time.Time{}, false
	}
	truncated := now.Truncate(time.Minute)
	floor := now.Add(-grace).Truncate(time.Minute)
	for ; !truncated.Before(floor); truncated = truncated.Add(-time.Minute) {
		if spec.matches(truncated) && truncated.After(anchorAt) {
			return truncated, true
		}
	}
	return time.Time{}, false
}
