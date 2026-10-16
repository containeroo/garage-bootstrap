package flags

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		args := []string{
			"--admin-url", "https://garage.example",
			"--config", "/config/bootstrap.yaml",
		}
		options, err := Parse(args, "test")
		require.NoError(t, err)
		assert.Equal(t, "https://garage.example", options.AdminURL.String())
		assert.Equal(t, ":8080", options.ListenAddress)
	})

	t.Run("custom listen address", func(t *testing.T) {
		t.Parallel()
		args := []string{
			"--admin-url", "https://garage.example",
			"--config", "/config/bootstrap.yaml",
			"--listen-address", "127.0.0.1:9000",
		}
		options, err := Parse(args, "test")
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1:9000", options.ListenAddress)
	})

	t.Run("rejects relative admin url", func(t *testing.T) {
		t.Parallel()
		args := []string{
			"--admin-url", "/admin",
			"--config", "/config/bootstrap.yaml",
		}
		_, err := Parse(args, "test")
		assert.Error(t, err)
	})

	t.Run("rejects removed check interval", func(t *testing.T) {
		t.Parallel()
		args := []string{
			"--admin-url", "https://garage.example",
			"--config", "/config/bootstrap.yaml",
			"--check-interval", "30s",
		}
		_, err := Parse(args, "test")
		assert.Error(t, err)
	})
}
