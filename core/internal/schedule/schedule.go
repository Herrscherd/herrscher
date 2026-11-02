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
func Validate(s Schedule, now time.Time) error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: needs a name", errInvalidSchedule)
	}
	if strings.TrimSpace(s.Task) == "" {
		return fmt.Errorf("%w: needs a task", errInvalidSchedule)
	}
	switch {
	case s.Session == "" && s.Agent == "":
		return fmt.Errorf("%w: needs a target: --session or --agent", errInvalidSchedule)
	case s.Session != "" && s.Agent != "":
		return fmt.Errorf("%w: takes one target: --session or --agent, not both", errInvalidSchedule)
	}
	if s.Project != "" && s.Agent == "" {
		return fmt.Errorf("%w: --project only means something with --agent, which opens the session", errInvalidSchedule)
	}
	switch {
	case s.Every == "" && s.Cron == "":
		return fmt.Errorf("%w: needs a cadence: --every or --cron", errInvalidSchedule)
	case s.Every != "" && s.Cron != "":
		return fmt.Errorf("%w: takes one cadence: --every or --cron, not both", errInvalidSchedule)
	}
	if _, err := graceOf(s); err != nil {
		return err
	}
	if s.Every != "" {
		_, err := period(s)
		return err
	}
	if _, err := parseCron(s.Cron); err != nil {
		return err
	}
	_, err := Next(s, now)
	return err
}

func period(s Schedule) (time.Duration, error) {
	d, err := time.ParseDuration(s.Every)
	if err != nil {
		return 0, fmt.Errorf("--every %q: %w", s.Every, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%w: --every %q must be positive", errInvalidSchedule, s.Every)
	}
	return d, nil
}

func graceOf(s Schedule) (time.Duration, error) {
	if s.Grace == "" {
		return DefaultGrace, nil
	}
	d, err := time.ParseDuration(s.Grace)
	if err != nil {
		return 0, fmt.Errorf("--grace %q: %w", s.Grace, err)
	}
	if d < minGrace {
		return 0, fmt.Errorf("%w: --grace %q must be at least %s", errInvalidSchedule, s.Grace, minGrace)
	}
	return d, nil
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
func Next(s Schedule, after time.Time) (time.Time, error) {
	if s.Every != "" {
		d, err := period(s)
		if err != nil {
			return time.Time{}, err
		}
		return after.Add(d), nil
	}
	spec, err := parseCron(s.Cron)
	if err != nil {
		return time.Time{}, err
	}
	t := after.Truncate(time.Minute).Add(time.Minute)
	deadline := after.Add(maxCronLookahead)
	for ; t.Before(deadline); t = t.Add(time.Minute) {
		if spec.matches(t) {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: %q within %s", errCronNoWindow, s.Cron, maxCronLookahead)
}

// NextWindow rend la prochaine fenetre a venir, comptee depuis l'ancre et non
// depuis l'instant ou la question est posee. La difference se voit sur --every :
// un horaire de 24h ancre a 9h ouvre sa prochaine fenetre demain a 9h, et non
// dans vingt-quatre heures a compter de maintenant. C'est ce que veut savoir qui
// lit `schedule list`.
//
// Un horaire deja du rend une fenetre passee, puisque c'est celle qu'il attend :
// a l'appelant de poser Due d'abord s'il veut distinguer les deux.
func NextWindow(s Schedule) (time.Time, error) {
	a, ok := anchor(s)
	if !ok {
		return time.Time{}, errNoAnchor
	}
	return Next(s, a)
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
func Window(s Schedule, now time.Time) (time.Time, bool) {
	grace, err := graceOf(s)
	if err != nil {
		return time.Time{}, false
	}
	return lastWindow(s, now, grace)
}

// Due dit si la prochaine fenetre est arrivee. C'est la question que la boucle
// pose a chaque tick.
func Due(s Schedule, now time.Time) bool {
	a, ok := anchor(s)
	if !ok {
		return false
	}
	next, err := Next(s, a)
	return err == nil && !next.After(now)
}

// Stale dit qu'une fenetre est arrivee mais qu'elle est trop vieille pour etre
// servie. Elle ne doit ni tirer ni rester en attente : la boucle realigne alors
// l'horaire sur maintenant. Sans ce troisieme cas, une fenetre que CatchUp
// refuse resterait due, et le tick suivant la tirerait quand meme, sans dire son
// retard : le digest de 9h serait rendu a 18h en se presentant comme celui du
// matin, ce que la grace existe precisement pour empecher.
func Stale(s Schedule, now time.Time) bool {
	if s.Paused || !Due(s, now) {
		return false
	}
	fire, _ := CatchUp(s, now)
	return !fire
}

// CatchUp decide du sort d'une fenetre ratee pendant que le daemon etait
// arrete. Il tire au plus une fois, sur la fenetre ratee la plus recente, et
// seulement si son retard tient dans le delai de grace. Rejouer chaque fenetre
// ratee facturerait N tours qui raisonnent tous sur un monde disparu.
func CatchUp(s Schedule, now time.Time) (bool, time.Duration) {
	if s.Paused {
		return false, 0
	}
	grace, err := graceOf(s)
	if err != nil {
		return false, 0
	}
	w, ok := lastWindow(s, now, grace)
	if !ok {
		return false, 0
	}
	late := now.Sub(w)
	if late > grace {
		return false, 0
	}
	return true, late
}

func lastWindow(s Schedule, now time.Time, grace time.Duration) (time.Time, bool) {
	a, ok := anchor(s)
	if !ok {
		return time.Time{}, false
	}
	if s.Every != "" {
		d, err := period(s)
		if err != nil {
			return time.Time{}, false
		}
		elapsed := now.Sub(a)
		if elapsed < d {
			return time.Time{}, false
		}
		return a.Add(elapsed.Truncate(d)), true
	}
	spec, err := parseCron(s.Cron)
	if err != nil {
		return time.Time{}, false
	}
	t := now.Truncate(time.Minute)
	floor := now.Add(-grace).Truncate(time.Minute)
	for ; !t.Before(floor); t = t.Add(-time.Minute) {
		if spec.matches(t) && t.After(a) {
			return t, true
		}
	}
	return time.Time{}, false
}
