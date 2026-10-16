// Package reconcile makes the declared Garage administrative state converge.
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gi8lino/garage-bootstrap/internal/backoff"
	"github.com/gi8lino/garage-bootstrap/internal/config"
	"github.com/gi8lino/garage-bootstrap/internal/garage"
)

// AdminAPI is the minimal Garage Admin API surface used by Reconciler.
type AdminAPI interface {
	GetKey(context.Context, string) (*garage.Key, error)
	ListKeys(context.Context) ([]garage.Key, error)
	ImportKey(context.Context, string, string, string) (*garage.Key, error)
	CreateKey(context.Context, string) (*garage.Key, error)
	UpdateKey(context.Context, string, string, bool) error
	GetBucket(context.Context, string) (*garage.Bucket, error)
	CreateBucket(context.Context, string) (*garage.Bucket, error)
	AllowBucketKey(context.Context, string, string, garage.Permissions) error
	DenyBucketKey(context.Context, string, string, garage.Permissions) error
}

// Reconciler converges Garage administrative state.
type Reconciler struct {
	// API manages Garage resources.
	API AdminAPI
	// Config declares desired resources.
	Config config.Config
	// Logger records reconciliation work.
	Logger *slog.Logger
	// DryRun reports changes without applying them.
	DryRun bool
}

// keyUpdate identifies a key update.
type keyUpdate struct {
	// key is the current key.
	key *garage.Key
	// declaration is the desired key.
	declaration config.User
}

// grantChange identifies a permission change.
type grantChange struct {
	// bucketID identifies the bucket.
	bucketID string
	// accessKeyID identifies the key.
	accessKeyID string
	// permissions are the changed permissions.
	permissions garage.Permissions
}

// Run reconciles configured keys, buckets, and grants.
func (r Reconciler) Run(ctx context.Context) error {
	if err := r.Config.Validate(); err != nil {
		return fmt.Errorf("validate configuration: %w", err)
	}
	if r.API == nil {
		return errors.New("reconciler API is required")
	}
	if r.Logger == nil {
		return errors.New("reconciler logger is required")
	}

	logger := r.Logger

	keys, err := r.ensureKeys(ctx, logger)
	if err != nil {
		return err
	}

	buckets, err := r.ensureBuckets(ctx, logger)
	if err != nil {
		return err
	}

	return r.ensureGrants(ctx, keys, buckets, logger)
}

// ensureKeys reconciles configured keys.
func (r Reconciler) ensureKeys(ctx context.Context, logger *slog.Logger) (map[string]*garage.Key, error) {
	keys := make(map[string]*garage.Key, len(r.Config.Users))

	for _, declaration := range r.Config.Users {
		key, err := r.ensureKey(ctx, declaration, logger)
		if err != nil {
			return nil, fmt.Errorf("reconcile key %q: %w", declaration.KeyName(), err)
		}
		keys[declaration.KeyName()] = key
	}

	return keys, nil
}

// ensureBuckets reconciles configured buckets.
func (r Reconciler) ensureBuckets(ctx context.Context, logger *slog.Logger) (map[string]*garage.Bucket, error) {
	buckets := make(map[string]*garage.Bucket, len(r.Config.Buckets))

	for _, declaration := range r.Config.Buckets {
		bucket, err := r.ensureBucket(ctx, declaration, logger)
		if err != nil {
			return nil, fmt.Errorf("reconcile bucket %q: %w", declaration.Name, err)
		}
		buckets[declaration.Name] = bucket
	}

	return buckets, nil
}

// ensureGrants reconciles configured grants.
func (r Reconciler) ensureGrants(
	ctx context.Context,
	keys map[string]*garage.Key,
	buckets map[string]*garage.Bucket,
	logger *slog.Logger,
) error {
	for _, declaration := range r.Config.Grants() {
		if err := r.ensureGrant(ctx, buckets[declaration.Bucket], keys[declaration.User], declaration, logger); err != nil {
			return fmt.Errorf("reconcile grant %q/%q: %w", declaration.Bucket, declaration.User, err)
		}
	}
	return nil
}

// ensureKey reconciles one key.
func (r Reconciler) ensureKey(
	ctx context.Context,
	declaration config.User,
	logger *slog.Logger,
) (*garage.Key, error) {
	key, err := r.findKey(ctx, declaration)
	if err != nil {
		return nil, err
	}
	if key != nil && keyMatchesDeclaration(key, declaration) {
		logger.Info(
			"skipping access key",
			"name", declaration.KeyName(),
			"reason", "already up to date",
		)
		return key, nil
	}
	if key == nil {
		if r.DryRun {
			return r.missingKey(declaration, logger), nil
		}
		key, err = r.createKey(ctx, declaration, logger)
		if err != nil {
			return nil, err
		}
	}
	return r.updateKey(ctx, key, declaration, logger)
}

// findKey finds a configured key.
func (r Reconciler) findKey(ctx context.Context, declaration config.User) (*garage.Key, error) {
	if declaration.AccessKey == "" {
		return r.keyByName(ctx, declaration.KeyName())
	}
	return retry(ctx, r.Logger, "get key", declaration.AccessKey, r.API.GetKey)
}

// missingKey returns a dry-run key.
func (r Reconciler) missingKey(declaration config.User, logger *slog.Logger) *garage.Key {
	if declaration.AccessKey != "" {
		logger.Info("would import access key",
			"name", declaration.KeyName(),
		)
	} else {
		logger.Info("would create access key",
			"name", declaration.KeyName(),
		)
	}
	return &garage.Key{
		AccessKeyID: declaration.AccessKey,
		Name:        declaration.KeyName(),
		Permissions: garage.KeyPermissions{CreateBucket: declaration.AllowCreateBucket},
	}
}

// createKey creates or imports a key.
func (r Reconciler) createKey(
	ctx context.Context,
	declaration config.User,
	logger *slog.Logger,
) (*garage.Key, error) {
	if declaration.AccessKey == "" {
		logger.Info("creating access key",
			"name", declaration.KeyName(),
		)

		return retry(ctx, r.Logger, "create key", declaration.KeyName(), r.API.CreateKey)
	}

	logger.Info("importing access key",
		"name", declaration.KeyName(),
	)
	return retry(ctx, r.Logger, "import key", declaration, r.importKey)
}

// importKey imports a configured key.
func (r Reconciler) importKey(ctx context.Context, declaration config.User) (*garage.Key, error) {
	return r.API.ImportKey(ctx, declaration.AccessKey, declaration.SecretKey, declaration.KeyName())
}

// updateKey applies key metadata.
func (r Reconciler) updateKey(
	ctx context.Context,
	key *garage.Key,
	declaration config.User,
	logger *slog.Logger,
) (*garage.Key, error) {
	if keyMatchesDeclaration(key, declaration) {
		return key, nil
	}
	if r.DryRun {
		logger.Info("would update access key",
			"name", declaration.KeyName(),
		)
		return key, nil
	}

	logger.Info("updating access key",
		"name", declaration.KeyName(),
	)
	change := keyUpdate{key: key, declaration: declaration}
	if err := retryVoid(ctx, r.Logger, "update key", change, r.applyKeyUpdate); err != nil {
		return nil, err
	}

	key.Name = declaration.KeyName()
	key.Permissions.CreateBucket = declaration.AllowCreateBucket

	return key, nil
}

// keyMatchesDeclaration reports whether the key already matches its declaration.
func keyMatchesDeclaration(key *garage.Key, declaration config.User) bool {
	return key.Name == declaration.KeyName() &&
		key.Permissions.CreateBucket == declaration.AllowCreateBucket
}

// applyKeyUpdate updates one key.
func (r Reconciler) applyKeyUpdate(ctx context.Context, change keyUpdate) error {
	return r.API.UpdateKey(
		ctx,
		change.key.AccessKeyID,
		change.declaration.KeyName(),
		change.declaration.AllowCreateBucket,
	)
}

// keyByName finds exactly one named key.
func (r Reconciler) keyByName(ctx context.Context, name string) (*garage.Key, error) {
	keys, err := retry(ctx, r.Logger, "list keys", name, r.listKeys)
	if err != nil {
		return nil, err
	}
	var match *garage.Key
	for i := range keys {
		if keys[i].Name != name {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("multiple Garage keys share configured name")
		}
		match = &keys[i]
	}
	return match, nil
}

// listKeys lists keys for a name lookup.
func (r Reconciler) listKeys(ctx context.Context, _ string) ([]garage.Key, error) {
	return r.API.ListKeys(ctx)
}

// ensureBucket reconciles one bucket.
func (r Reconciler) ensureBucket(
	ctx context.Context,
	declaration config.Bucket,
	logger *slog.Logger,
) (*garage.Bucket, error) {
	bucket, err := retry(ctx, r.Logger, "get bucket", declaration.Name, r.API.GetBucket)
	if err != nil {
		return nil, err
	}
	if bucket != nil {
		logger.Info(
			"skipping bucket creation",
			"name", declaration.Name,
			"reason", "already exists",
		)
		return bucket, nil
	}
	if r.DryRun {
		logger.Info("would create bucket",
			"name", declaration.Name,
		)

		return &garage.Bucket{}, nil
	}
	logger.Info("creating bucket",
		"name", declaration.Name,
	)

	return retry(ctx, r.Logger, "create bucket", declaration.Name, r.API.CreateBucket)
}

// ensureGrant reconciles one grant.
func (r Reconciler) ensureGrant(
	ctx context.Context,
	bucket *garage.Bucket,
	key *garage.Key,
	declaration config.Grant,
	logger *slog.Logger,
) error {
	desired := garage.Permissions{
		Read:  declaration.Read,
		Write: declaration.Write,
		Owner: declaration.Owner,
	}

	if r.DryRun {
		logger.Info(
			"would reconcile bucket grant",
			"bucket", declaration.Bucket,
			"key", declaration.User,
			"read", desired.Read,
			"write", desired.Write,
			"owner", desired.Owner,
		)
		return nil
	}

	logger.Info("reconciling bucket grant",
		"bucket", declaration.Bucket,
		"key", declaration.User,
	)
	allow := desired
	deny := garage.Permissions{Read: !desired.Read, Write: !desired.Write, Owner: !desired.Owner}

	if allow.Read || allow.Write || allow.Owner {
		change := grantChange{bucketID: bucket.ID, accessKeyID: key.AccessKeyID, permissions: allow}
		if err := retryVoid(ctx, r.Logger, "allow bucket key", change, r.allowBucketKey); err != nil {
			return err
		}
	}
	if deny.Read || deny.Write || deny.Owner {
		change := grantChange{bucketID: bucket.ID, accessKeyID: key.AccessKeyID, permissions: deny}
		if err := retryVoid(ctx, r.Logger, "deny bucket key", change, r.denyBucketKey); err != nil {
			return err
		}
	}
	return nil
}

// allowBucketKey applies allowed permissions.
func (r Reconciler) allowBucketKey(ctx context.Context, change grantChange) error {
	return r.API.AllowBucketKey(ctx, change.bucketID, change.accessKeyID, change.permissions)
}

// denyBucketKey applies denied permissions.
func (r Reconciler) denyBucketKey(ctx context.Context, change grantChange) error {
	return r.API.DenyBucketKey(ctx, change.bucketID, change.accessKeyID, change.permissions)
}

// retryable reports whether an error can be retried.
type retryable interface{ Retryable() bool }

// retry retries a transient Admin API call until context cancellation.
func retry[Input, Output any](
	ctx context.Context,
	logger *slog.Logger,
	operation string,
	input Input,
	call func(context.Context, Input) (Output, error),
) (Output, error) {
	var zero Output
	for attempt := 1; ; attempt++ {
		result, err := call(ctx, input)
		if err == nil {
			return result, nil
		}
		if !isRetryable(err) {
			return zero, fmt.Errorf("%s: %w", operation, err)
		}
		delay := retryDelay(attempt)
		logger.Warn(
			"Garage Admin API request failed; retrying",
			"operation", operation,
			"attempt", attempt,
			"retry_in", delay,
			"error", err,
		)
		if err := wait(ctx, delay); err != nil {
			return zero, err
		}
	}
}

// retryVoid retries a transient Admin API call until context cancellation.
func retryVoid[Input any](
	ctx context.Context,
	logger *slog.Logger,
	operation string,
	input Input,
	call func(context.Context, Input) error,
) error {
	for attempt := 1; ; attempt++ {
		err := call(ctx, input)
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			return fmt.Errorf("%s: %w", operation, err)
		}
		delay := retryDelay(attempt)
		logger.Warn(
			"Garage Admin API request failed; retrying",
			"operation", operation,
			"attempt", attempt,
			"retry_in", delay,
			"error", err,
		)
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
}

// isRetryable reports whether err supports retrying.
func isRetryable(err error) bool {
	var retriable retryable
	return errors.As(err, &retriable) && retriable.Retryable()
}

// retryDelay returns the capped exponential delay for attempt.
func retryDelay(attempt int) time.Duration {
	return backoff.NextInterval(backoff.ModeExponential, 100*time.Millisecond, attempt, time.Second)
}

// wait waits for delay or context cancellation.
func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
