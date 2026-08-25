package schedule

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DefaultGrace est le retard maximum qu'une fenetre ratee peut avoir et etre
// quand meme tiree au demarrage du daemon. Un seul chiffre, court et
// previsible, plutot qu'une regle derivee de la periode : derivee, elle ferait
// toujours tirer un horaire quotidien et jamais un horaire aux cinq minutes.
// Un digest de 9h recu a 9h45 sert encore, le meme a 18h derange.
const DefaultGrace = time.Hour

// maxCronLookahead borne la recherche de la prochaine fenetre cron. Une
// expression que le calendrier ne satisfait jamais, "0 0 30 2 *" par exemple,
// doit rendre une erreur au lieu de boucler.
const maxCronLookahead = 366 * 24 * time.Hour

// Schedule est un horaire persiste. Session et Agent sont deux cibles
// mutuellement exclusives, Every et Cron deux cadences mutuellement exclusives.
type Schedule struct {
	Name    string `json:"name"`
	Session string `json:"session,omitempty"` // cible session, exclusive d'Agent
	Agent   string `json:"agent,omitempty"`   // cible agent, exclusive de Session
	Project string `json:"project,omitempty"` // sous-repertoire workspace, cible agent seulement
	Task    string `json:"task"`
	Every   string `json:"every,omitempty"` // duree lue par time.ParseDuration
	Cron    string `json:"cron,omitempty"`  // expression a cinq champs
	Grace   string `json:"grace,omitempty"` // duree ; vide vaut DefaultGrace
	Paused  bool   `json:"paused,omitempty"`
	// LastRun est le dernier tir qui a effectivement atteint une session, en
	// RFC3339. Vide veut dire jamais tire, et l'ancre est alors CreatedAt.
	LastRun   string `json:"lastRun,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// SessionName est le nom de la session qu'un horaire a cible agent possede. Il
// derive du nom de l'horaire, donc il est stable d'un tick au suivant : c'est
// ce qui permet de retrouver la session plutot que d'en creer une par tir.
func SessionName(s Schedule) string { return "schedule-" + s.Name }

// Validate porte toutes les gardes de la creation. Elles tombent ici, une fois,
// plutot qu'au tir : un horaire impossible doit etre refuse a la face de qui
// l'ecrit, pas echouer en silence chaque minute.
func Validate(s Schedule) error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("schedule needs a name")
	}
	if strings.TrimSpace(s.Task) == "" {
		return errors.New("schedule needs a task")
	}
	switch {
	case s.Session == "" && s.Agent == "":
		return errors.New("schedule needs a target: --session or --agent")
	case s.Session != "" && s.Agent != "":
		return errors.New("schedule takes one target: --session or --agent, not both")
	}
	switch {
	case s.Every == "" && s.Cron == "":
		return errors.New("schedule needs a cadence: --every or --cron")
	case s.Every != "" && s.Cron != "":
		return errors.New("schedule takes one cadence: --every or --cron, not both")
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
	// Un aller-retour complet, parce que parser accepte "0 0 30 2 *", que le
	// calendrier ne satisfait jamais : seule une recherche de fenetre le
	// decouvre. La question posee est donc "cette expression tire-t-elle dans
	// l'annee qui vient", et un horaire quadriennal comme le 29 fevrier est
	// refuse de ce fait. C'est voulu : personne n'ecrit un cron pour 2028.
	_, err := Next(s, time.Now())
	return err
}

// period lit la cadence d'un horaire --every.
func period(s Schedule) (time.Duration, error) {
	d, err := time.ParseDuration(s.Every)
	if err != nil {
		return 0, fmt.Errorf("--every %q: %w", s.Every, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("--every %q: must be positive", s.Every)
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
	if d < 0 {
		return 0, fmt.Errorf("--grace %q: must not be negative", s.Grace)
	}
	return d, nil
}

// anchor est le point d'ou la prochaine fenetre se compte : le dernier tir
// livre, ou a defaut la creation. Ni l'un ni l'autre veut dire qu'il n'y a rien
// d'ou compter, et un horaire sans ancre ne tire pas.
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
	return time.Time{}, fmt.Errorf("cron %q: no window within %s", s.Cron, maxCronLookahead)
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

// lastWindow rend la fenetre la plus recente a `now` ou avant, quand il en
// existe une strictement apres l'ancre. Les deux cadences se traitent
// differemment pour rester bornees : --every par arithmetique, --cron par une
// marche arriere limitee au delai de grace, puisqu'une fenetre plus vieille que
// la grace serait de toute facon rejetee.
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
		return a.Add((elapsed / d) * d), true
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
