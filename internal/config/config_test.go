package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMinIOStyleConfigAndResolvePolicies(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bootstrap.yaml")
	data := []byte(`users:
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
buckets:
  - name: db-backups
  - name: media
`)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Users, 3)
	assert.Equal(t, "GKONE", cfg.Users[0].KeyName())
	assert.Equal(t, "generated", cfg.Users[2].KeyName())

	assert.Equal(t, []Grant{
		{User: "GKONE", Bucket: "db-backups", Read: true, Write: true},
		{User: "GKONE", Bucket: "media"},
		{User: "GKTWO", Bucket: "db-backups", Read: true, Write: true},
		{User: "GKTWO", Bucket: "media"},
		{User: "generated", Bucket: "db-backups", Read: true},
		{User: "generated", Bucket: "media"},
	}, cfg.Grants())
}

func TestLoadRejectsInvalidYAML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown field", "users:\n  - name: backup\n    unknown: true\n", "field unknown not found"},
		{"unknown nested field", "policies:\n  - name: p\n    buckets:\n      - name: backup\n        action: true\n", "field action not found"},
		{"removed top-level grants", "grants: []\n", "field grants not found"},
		{"removed top-level keys", "keys: []\n", "field keys not found"},
		{"multiple documents", "users: []\n---\nbuckets: []\n", "multiple YAML documents"},
		{"duplicate YAML property", "users: []\nusers: []\n", "already defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "bootstrap.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))
			_, err := Load(path)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidateRejectsInvalidDeclarations(t *testing.T) {
	t.Parallel()
	valid := func() Config {
		return Config{
			Users:    []User{{AccessKey: "GKONE", SecretKey: "secret", Policy: "p"}},
			Policies: []Policy{{Name: "p", Buckets: []PolicyBucket{{Name: "backups", Read: true}}}},
			Buckets:  []Bucket{{Name: "backups"}},
		}
	}
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"duplicate user", func(c *Config) { c.Users = append(c.Users, c.Users[0]) }, "duplicate name"},
		{"duplicate access key with separate name", func(c *Config) {
			c.Users = append(c.Users, User{Name: "second", AccessKey: "GKONE", SecretKey: "different", Policy: "p"})
		}, "duplicate accessKey"},
		{"missing secret", func(c *Config) { c.Users[0].SecretKey = "" }, "must be provided together"},
		{"missing access key", func(c *Config) { c.Users[0].AccessKey = "" }, "must be provided together"},
		{"generated user without name", func(c *Config) { c.Users[0].AccessKey, c.Users[0].SecretKey = "", "" }, "name is required"},
		{"unknown policy", func(c *Config) { c.Users[0].Policy = "missing" }, "unknown policy"},
		{"duplicate policy", func(c *Config) { c.Policies = append(c.Policies, c.Policies[0]) }, "duplicate name"},
		{"missing policy name", func(c *Config) { c.Policies[0].Name = "" }, "name is required"},
		{"unknown bucket", func(c *Config) { c.Policies[0].Buckets[0].Name = "missing" }, "unknown bucket"},
		{"duplicate policy bucket", func(c *Config) {
			c.Policies[0].Buckets = append(c.Policies[0].Buckets, PolicyBucket{Name: "backups"})
		}, "duplicate bucket"},
		{"duplicate bucket", func(c *Config) { c.Buckets = append(c.Buckets, c.Buckets[0]) }, "duplicate name"},
		{"missing bucket name", func(c *Config) { c.Buckets[0].Name = "" }, "name is required"},
		{"wildcard bucket", func(c *Config) { c.Buckets[0].Name = "backup-*" }, "wildcards are not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := valid()
			tt.edit(&cfg)
			assert.ErrorContains(t, cfg.Validate(), tt.want)
		})
	}
}

func TestGeneratedUserAndEmptyPolicy(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Users:    []User{{Name: "automatically-generated", Policy: "none"}},
		Policies: []Policy{{Name: "none"}},
		Buckets:  []Bucket{{Name: "backups"}},
	}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, []Grant{{User: "automatically-generated", Bucket: "backups"}}, cfg.Grants())
}
