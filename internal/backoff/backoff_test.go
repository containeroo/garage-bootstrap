package backoff

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNextInterval(t *testing.T) {
	t.Parallel()

	t.Run("linear", func(t *testing.T) {
		t.Parallel()

		base := 100 * time.Millisecond
		interval := NextInterval(ModeExponential, base, 3, time.Second)
		expected := 400 * time.Millisecond

		assert.Equal(t, expected, interval)
	})

	t.Run("exponential", func(t *testing.T) {
		t.Parallel()

		base := 100 * time.Millisecond
		interval := NextInterval(ModeExponential, base, 8, 500*time.Millisecond)
		expected := 500 * time.Millisecond

		assert.Equal(t, expected, interval)
	})
}
