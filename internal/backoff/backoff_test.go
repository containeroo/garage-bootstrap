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

		interval := NextInterval(ModeLinear, base, 3, time.Second)

		assert.Equal(t, base, interval)
	})

	t.Run("exponential", func(t *testing.T) {
		t.Parallel()

		base := 100 * time.Millisecond
		expected := 400 * time.Millisecond

		interval := NextInterval(ModeExponential, base, 3, time.Second)

		assert.Equal(t, expected, interval)
	})

	t.Run("exponential capped", func(t *testing.T) {
		t.Parallel()

		base := 100 * time.Millisecond
		limit := 500 * time.Millisecond

		interval := NextInterval(ModeExponential, base, 8, limit)

		assert.Equal(t, limit, interval)
	})
}
