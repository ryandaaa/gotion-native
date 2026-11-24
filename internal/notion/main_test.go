package notion

import (
	"os"
	"testing"

	"gotion/internal/tr"
)

func TestMain(m *testing.M) {
	tr.Set(tr.ID) // the tests read the texts in Indonesian
	os.Exit(m.Run())
}
