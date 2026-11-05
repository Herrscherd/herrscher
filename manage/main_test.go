package manage

import (
	"os"
	"testing"

	"github.com/Herrscherd/herrscher/core/language"
)

func TestMain(m *testing.M) {
	if err := os.Setenv(language.EnvVar, "en"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
