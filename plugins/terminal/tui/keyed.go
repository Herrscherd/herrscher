package tui

type keyedError struct {
	cause  error
	key    string
	params map[string]any
}

func (e keyedError) Error() string {
	return newTranslator().t(e.key, e.params)
}

func (e keyedError) Unwrap() error {
	return e.cause
}
