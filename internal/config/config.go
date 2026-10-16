// Package config loads and validates bootstrap declarations.
package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config declares the Garage resources and their desired access policies.
type Config struct {
	// Users declares access keys and their assigned policies.
	Users []User `yaml:"users"`
	// Policies defines reusable permissions for named buckets.
	Policies []Policy `yaml:"policies"`
	// Buckets declares buckets managed by this configuration.
	Buckets []Bucket `yaml:"buckets"`
}

// User associates a Garage access key with one bootstrap policy.
type User struct {
	// Name optionally names an imported key and is required for generated keys.
	Name string `yaml:"name"`
	// AccessKey is the access key ID when importing explicit credentials.
	AccessKey string `yaml:"accessKey"`
	// SecretKey is the corresponding secret for an imported key.
	SecretKey string `yaml:"secretKey"`
	// Policy references a named bootstrap policy.
	Policy string `yaml:"policy"`
	// AllowCreateBucket controls Garage's key-level create-bucket permission.
	AllowCreateBucket bool `yaml:"allowCreateBucket"`
}

// KeyName returns the managed Garage key name, defaulting to its access key ID.
func (u User) KeyName() string {
	if u.Name != "" {
		return u.Name
	}
	return u.AccessKey
}

// Policy defines bucket permissions shared by one or more users.
type Policy struct {
	// Name identifies the policy in user declarations.
	Name string `yaml:"name"`
	// Buckets lists per-bucket permission sets; names must match declared buckets.
	Buckets []PolicyBucket `yaml:"buckets"`
}

// PolicyBucket is a policy's exact permission set for one Garage bucket.
type PolicyBucket struct {
	// Name identifies a declared bucket.
	Name string `yaml:"name"`
	// Read allows reading objects and bucket contents.
	Read bool `yaml:"read"`
	// Write allows writing and deleting objects.
	Write bool `yaml:"write"`
	// Owner allows bucket owner operations.
	Owner bool `yaml:"owner"`
}

// Bucket declares a Garage bucket by its global alias.
type Bucket struct {
	// Name is the global alias.
	Name string `yaml:"name"`
}

// Grant is a resolved exact permission set for a user on a managed bucket.
type Grant struct {
	// Bucket identifies the managed bucket.
	Bucket string
	// User identifies the managed access key by its Garage name.
	User string
	// Read permits reads.
	Read bool
	// Write permits writes.
	Write bool
	// Owner permits owner operations.
	Owner bool
}

// Load parses a single YAML document and validates all references before use.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer f.Close() // nolint:errcheck

	var cfg Config
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("decode configuration: multiple YAML documents are not supported")
		}
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks configuration declarations and their relationships.
func (c Config) Validate() error {
	buckets, err := c.validateBuckets()
	if err != nil {
		return err
	}

	policies, err := c.validatePolicies(buckets)
	if err != nil {
		return err
	}

	return c.validateUsers(policies)
}

// validateBuckets checks bucket declarations and returns their names.
func (c Config) validateBuckets() (map[string]struct{}, error) {
	buckets := make(map[string]struct{}, len(c.Buckets))

	for i, bucket := range c.Buckets {
		if bucket.Name == "" {
			return nil, fmt.Errorf("buckets[%d]: name is required", i)
		}
		if !validBucketName(bucket.Name) {
			return nil, fmt.Errorf("buckets[%d]: invalid bucket name %q", i, bucket.Name)
		}
		if _, exists := buckets[bucket.Name]; exists {
			return nil, fmt.Errorf("buckets[%d]: duplicate name %q", i, bucket.Name)
		}

		buckets[bucket.Name] = struct{}{}
	}

	return buckets, nil
}

// validatePolicies checks policy declarations and their bucket references.
func (c Config) validatePolicies(buckets map[string]struct{}) (map[string]struct{}, error) {
	policies := make(map[string]struct{}, len(c.Policies))

	for i, policy := range c.Policies {
		if policy.Name == "" {
			return nil, fmt.Errorf("policies[%d]: name is required", i)
		}
		if _, exists := policies[policy.Name]; exists {
			return nil, fmt.Errorf("policies[%d]: duplicate name %q", i, policy.Name)
		}

		policies[policy.Name] = struct{}{}
		seen := make(map[string]struct{}, len(policy.Buckets))

		for j, bucket := range policy.Buckets {
			if !validBucketName(bucket.Name) {
				return nil, fmt.Errorf("policies[%d].buckets[%d]: invalid bucket name %q", i, j, bucket.Name)
			}
			if _, exists := buckets[bucket.Name]; !exists {
				return nil, fmt.Errorf("policies[%d].buckets[%d]: unknown bucket %q", i, j, bucket.Name)
			}
			if _, exists := seen[bucket.Name]; exists {
				return nil, fmt.Errorf("policies[%d].buckets[%d]: duplicate bucket %q", i, j, bucket.Name)
			}

			seen[bucket.Name] = struct{}{}
		}
	}

	return policies, nil
}

// validBucketName reports whether name meets Garage's bucket alias rules.
func validBucketName(name string) bool {
	if len(name) < 3 || len(name) > 63 || net.ParseIP(name) != nil {
		return false
	}
	if name[0] == '.' || name[0] == '-' || name[len(name)-1] == '.' || name[len(name)-1] == '-' {
		return false
	}
	if strings.HasPrefix(name, "xn--") || strings.Contains(name, ".xn--") || strings.HasSuffix(name, "-s3alias") {
		return false
	}
	for _, character := range name {
		if character != '.' && character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// validateUsers checks user credentials, uniqueness, and policy references.
func (c Config) validateUsers(policies map[string]struct{}) error {
	users := make(map[string]struct{}, len(c.Users))
	accessKeys := make(map[string]struct{}, len(c.Users))

	for i, user := range c.Users {
		if (user.AccessKey == "") != (user.SecretKey == "") {
			return fmt.Errorf("users[%d]: accessKey and secretKey must be provided together", i)
		}

		name := user.KeyName()
		if name == "" {
			return fmt.Errorf("users[%d]: name is required when accessKey and secretKey are omitted", i)
		}
		if _, exists := users[name]; exists {
			return fmt.Errorf("users[%d]: duplicate name %q", i, name)
		}
		users[name] = struct{}{}

		if user.AccessKey != "" {
			if _, exists := accessKeys[user.AccessKey]; exists {
				return fmt.Errorf("users[%d]: duplicate accessKey %q", i, user.AccessKey)
			}
			accessKeys[user.AccessKey] = struct{}{}
		}

		if _, exists := policies[user.Policy]; !exists {
			return fmt.Errorf("users[%d]: unknown policy %q", i, user.Policy)
		}
	}

	return nil
}

// Grants resolves policies into exact permissions for every managed user/bucket pair.
// Missing bucket entries mean no permissions; unmanaged buckets are left untouched.
// The input slice order determines reconciliation order.
func (c Config) Grants() []Grant {
	policies := make(map[string]map[string]PolicyBucket, len(c.Policies))
	for _, policy := range c.Policies {
		permissions := make(map[string]PolicyBucket, len(policy.Buckets))
		for _, bucket := range policy.Buckets {
			permissions[bucket.Name] = bucket
		}
		policies[policy.Name] = permissions
	}

	grants := make([]Grant, 0, len(c.Users)*len(c.Buckets))
	for _, user := range c.Users {
		for _, bucket := range c.Buckets {
			permissions := policies[user.Policy][bucket.Name]
			grants = append(grants, Grant{
				Bucket: bucket.Name,
				User:   user.KeyName(),
				Read:   permissions.Read,
				Write:  permissions.Write,
				Owner:  permissions.Owner,
			})
		}
	}
	return grants
}
