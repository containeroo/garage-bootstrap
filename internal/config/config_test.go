package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigAndResolvePolicies(t *testing.T) {
	t.Parallel()

	data := `users:
  - accessKey: GKONE
    secretKey: secret-one
    policy: cnpg
  - accessKey: GKTWO
    secretKey: secret-two
    policy: cnpg
  - name: generated
    policy: readonly
policies:
  - name: cnpg
    buckets:
      - name: db-backups
        read: true
        write: true
  - name: readonly
    buckets:
      - name: db-backups
        read: true
`

	cfg, err := loadTestConfig(t, data)

	require.NoError(t, err)
	require.Len(t, cfg.Users, 3)
	assert.Equal(t, "GKONE", cfg.Users[0].KeyName())
	assert.Equal(t, "generated", cfg.Users[2].KeyName())
	assert.Equal(t, []Grant{
		{
			User:   "GKONE",
			Bucket: "db-backups",
			Read:   true,
			Write:  true,
		},
		{
			User:   "GKTWO",
			Bucket: "db-backups",
			Read:   true,
			Write:  true,
		},
		{
			User:   "generated",
			Bucket: "db-backups",
			Read:   true,
		},
	}, cfg.Grants())
	assert.Equal(t, []string{"db-backups"}, cfg.Buckets())
}

func TestLoadRejectsInvalidYAML(t *testing.T) {
	t.Parallel()

	t.Run("unknown user field", func(t *testing.T) {
		t.Parallel()

		_, err := loadTestConfig(t, "users:\n  - name: backup\n    unknown: true\n")

		require.ErrorContains(t, err, "field unknown not found")
	})

	t.Run("unknown policy bucket field", func(t *testing.T) {
		t.Parallel()

		_, err := loadTestConfig(t, "policies:\n  - name: p\n    buckets:\n      - name: backup\n        action: true\n")

		require.ErrorContains(t, err, "field action not found")
	})

	t.Run("top-level grants are not supported", func(t *testing.T) {
		t.Parallel()

		_, err := loadTestConfig(t, "grants: []\n")

		require.ErrorContains(t, err, "field grants not found")
	})

	t.Run("top-level keys are not supported", func(t *testing.T) {
		t.Parallel()

		_, err := loadTestConfig(t, "keys: []\n")

		require.ErrorContains(t, err, "field keys not found")
	})

	t.Run("top-level buckets are not supported", func(t *testing.T) {
		t.Parallel()

		_, err := loadTestConfig(t, "buckets: []\n")

		require.ErrorContains(t, err, "field buckets not found")
	})

	t.Run("multiple documents", func(t *testing.T) {
		t.Parallel()

		_, err := loadTestConfig(t, "users: []\n---\npolicies: []\n")

		require.ErrorContains(t, err, "expected single document, found multiple")
	})

	t.Run("duplicate YAML property", func(t *testing.T) {
		t.Parallel()

		_, err := loadTestConfig(t, "users: []\nusers: []\n")

		require.ErrorContains(t, err, "already defined")
	})
}

func TestValidateRejectsInvalidDeclarations(t *testing.T) {
	t.Parallel()

	t.Run("duplicate user", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Users = append(cfg.Users, cfg.Users[0])

		assert.ErrorContains(t, cfg.Validate(), "duplicate name")
	})

	t.Run("duplicate access key with a different name", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Users = append(cfg.Users, User{
			Name:      "second",
			AccessKey: "GKONE",
			SecretKey: "different",
			Policy:    "p",
		})

		assert.ErrorContains(t, cfg.Validate(), "duplicate accessKey")
	})

	t.Run("missing secret", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Users[0].SecretKey = ""

		assert.ErrorContains(t, cfg.Validate(), "must be provided together")
	})

	t.Run("missing access key", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Users[0].AccessKey = ""

		assert.ErrorContains(t, cfg.Validate(), "must be provided together")
	})

	t.Run("generated user without name", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Users[0].AccessKey = ""
		cfg.Users[0].SecretKey = ""

		assert.ErrorContains(t, cfg.Validate(), "name is required")
	})

	t.Run("unknown policy", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Users[0].Policy = "missing"

		assert.ErrorContains(t, cfg.Validate(), "unknown policy")
	})

	t.Run("duplicate policy", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Policies = append(cfg.Policies, cfg.Policies[0])

		assert.ErrorContains(t, cfg.Validate(), "duplicate name")
	})

	t.Run("missing policy name", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Policies[0].Name = ""

		assert.ErrorContains(t, cfg.Validate(), "name is required")
	})

	t.Run("duplicate policy bucket", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Policies[0].Buckets = append(cfg.Policies[0].Buckets, PolicyBucket{
			Name: "backups",
		})

		assert.ErrorContains(t, cfg.Validate(), "duplicate bucket")
	})

	t.Run("missing bucket name", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Policies[0].Buckets[0].Name = ""

		assert.ErrorContains(t, cfg.Validate(), "invalid bucket name")
	})

	t.Run("invalid bucket name", func(t *testing.T) {
		t.Parallel()

		cfg := validTestConfig()
		cfg.Policies[0].Buckets[0].Name = "backup-*"

		assert.ErrorContains(t, cfg.Validate(), "invalid bucket name")
	})
}

func TestGeneratedUserAndEmptyPolicy(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Users: []User{
			{
				Name:   "automatically-generated",
				Policy: "none",
			},
		},
		Policies: []Policy{
			{Name: "none"},
		},
	}

	require.NoError(t, cfg.Validate())
	assert.Empty(t, cfg.Buckets())
	assert.Empty(t, cfg.Grants())
}

// loadTestConfig writes and loads one isolated YAML document.
func loadTestConfig(t *testing.T, content string) (Config, error) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bootstrap.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return Load(path)
}

// validTestConfig returns an independent starting point for validation tests.
func validTestConfig() Config {
	return Config{
		Users: []User{
			{
				AccessKey: "GKONE",
				SecretKey: "secret",
				Policy:    "p",
			},
		},
		Policies: []Policy{
			{
				Name: "p",
				Buckets: []PolicyBucket{
					{
						Name: "backups",
						Read: true,
					},
				},
			},
		},
	}
}
