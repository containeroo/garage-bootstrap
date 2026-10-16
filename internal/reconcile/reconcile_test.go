package reconcile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/gi8lino/garage-bootstrap/internal/config"
	"github.com/gi8lino/garage-bootstrap/internal/garage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type permissionKey struct {
	bucketID string
	keyID    string
}

type fakeAPI struct {
	keys        map[string]*garage.Key
	buckets     map[string]*garage.Bucket
	permissions map[permissionKey]garage.Permissions
	created     int
	imported    int
	updated     int
	calls       int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		keys:        make(map[string]*garage.Key),
		buckets:     make(map[string]*garage.Bucket),
		permissions: make(map[permissionKey]garage.Permissions),
	}
}

func newReconciler(api *fakeAPI, cfg config.Config) Reconciler {
	return Reconciler{
		API:    api,
		Config: cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func (f *fakeAPI) GetKey(_ context.Context, id string) (*garage.Key, error) {
	f.calls++
	return f.keys[id], nil
}

func (f *fakeAPI) ListKeys(context.Context) ([]garage.Key, error) {
	f.calls++
	keys := make([]garage.Key, 0, len(f.keys))
	for _, key := range f.keys {
		keys = append(keys, *key)
	}
	return keys, nil
}

func (f *fakeAPI) ImportKey(_ context.Context, id, secret, name string) (*garage.Key, error) {
	f.calls++
	if secret == "" {
		return nil, fmt.Errorf("secret is required")
	}
	f.imported++
	key := &garage.Key{AccessKeyID: id, Name: name}
	f.keys[id] = key
	return key, nil
}

func (f *fakeAPI) CreateKey(_ context.Context, name string) (*garage.Key, error) {
	f.calls++
	f.created++
	id := "generated-" + name
	key := &garage.Key{AccessKeyID: id, Name: name}
	f.keys[id] = key
	return key, nil
}

func (f *fakeAPI) UpdateKey(_ context.Context, id, name string, allowCreateBucket bool) error {
	f.calls++
	f.updated++
	key := f.keys[id]
	key.Name = name
	key.Permissions.CreateBucket = allowCreateBucket
	return nil
}

func (f *fakeAPI) GetBucket(_ context.Context, alias string) (*garage.Bucket, error) {
	f.calls++
	return f.buckets[alias], nil
}

func (f *fakeAPI) CreateBucket(_ context.Context, alias string) (*garage.Bucket, error) {
	f.calls++
	bucket := &garage.Bucket{ID: "bucket-" + alias}
	f.buckets[alias] = bucket
	return bucket, nil
}

func (f *fakeAPI) AllowBucketKey(_ context.Context, bucketID, keyID string, p garage.Permissions) error {
	f.calls++
	id := permissionKey{bucketID, keyID}
	current := f.permissions[id]
	current.Read = current.Read || p.Read
	current.Write = current.Write || p.Write
	current.Owner = current.Owner || p.Owner
	f.permissions[id] = current
	return nil
}

func TestRunReconcilesExplicitPolicyGrants(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	api.permissions[permissionKey{"bucket-backups", "GKONE"}] = garage.Permissions{Owner: true}
	api.permissions[permissionKey{"bucket-media", "GKONE"}] = garage.Permissions{Read: true, Write: true, Owner: true}
	// A bucket absent from the configuration must not be affected.
	api.permissions[permissionKey{"bucket-unmanaged", "GKONE"}] = garage.Permissions{Read: true, Write: true}

	cfg := config.Config{
		Users: []config.User{
			{AccessKey: "GKONE", SecretKey: "first", Policy: "cnpg", AllowCreateBucket: false},
			{AccessKey: "GKTWO", SecretKey: "second", Policy: "cnpg"},
			{Name: "readonly", Policy: "reader"},
		},
		Policies: []config.Policy{
			{Name: "cnpg", Buckets: []config.PolicyBucket{{Name: "backups", Read: true, Write: true}}},
			{Name: "reader", Buckets: []config.PolicyBucket{{Name: "backups", Read: true}}},
		},
	}
	r := newReconciler(api, cfg)
	require.NoError(t, r.Run(context.Background()))
	assert.Equal(t, 2, api.imported)
	assert.Equal(t, 1, api.created)
	assert.Equal(t, garage.Permissions{Read: true, Write: true, Owner: true}, api.permissions[permissionKey{"bucket-backups", "GKONE"}])
	assert.Equal(t, garage.Permissions{Read: true, Write: true}, api.permissions[permissionKey{"bucket-backups", "GKTWO"}])
	assert.Equal(t, garage.Permissions{Read: true}, api.permissions[permissionKey{"bucket-backups", "generated-readonly"}])
	assert.Equal(t, garage.Permissions{Read: true, Write: true, Owner: true}, api.permissions[permissionKey{"bucket-media", "GKONE"}])
	assert.Equal(t, garage.Permissions{Read: true, Write: true}, api.permissions[permissionKey{"bucket-unmanaged", "GKONE"}])

	// Starting the reconciler again must not create or import more access keys.
	require.NoError(t, r.Run(context.Background()))
	assert.Equal(t, 2, api.imported)
	assert.Equal(t, 1, api.created)
}

func TestRunLogsSkippedResources(t *testing.T) {
	t.Parallel()

	api := newFakeAPI()
	api.keys["GKONE"] = &garage.Key{AccessKeyID: "GKONE", Name: "backup"}
	api.buckets["backups"] = &garage.Bucket{ID: "bucket-backups"}
	cfg := config.Config{
		Users:    []config.User{{AccessKey: "GKONE", SecretKey: "secret", Name: "backup", Policy: "none"}},
		Policies: []config.Policy{{Name: "none", Buckets: []config.PolicyBucket{{Name: "backups"}}}},
	}
	var logs bytes.Buffer
	r := newReconciler(api, cfg)
	r.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	require.NoError(t, r.Run(context.Background()))
	assert.Contains(t, logs.String(), "skipping access key")
	assert.Contains(t, logs.String(), "skipping bucket creation")
}

func TestRunSkipsSatisfiedGrant(t *testing.T) {
	t.Parallel()

	api := newFakeAPI()
	api.keys["GKONE"] = &garage.Key{AccessKeyID: "GKONE", Name: "backup"}
	api.buckets["backups"] = &garage.Bucket{
		ID: "bucket-backups",
		Keys: []garage.BucketKey{{
			AccessKeyID: "GKONE",
			Permissions: garage.Permissions{
				Read:  true,
				Write: true,
			},
		}},
	}
	cfg := config.Config{
		Users: []config.User{{AccessKey: "GKONE", SecretKey: "secret", Name: "backup", Policy: "backup"}},
		Policies: []config.Policy{{
			Name: "backup",
			Buckets: []config.PolicyBucket{{
				Name:  "backups",
				Read:  true,
				Write: true,
			}},
		}},
	}
	var logs bytes.Buffer
	r := newReconciler(api, cfg)
	r.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	require.NoError(t, r.Run(context.Background()))
	assert.Empty(t, api.permissions)
	assert.Contains(t, logs.String(), "skipping bucket grant")
}

func TestRunDoesNotCallAPIForInvalidConfig(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	cfg := config.Config{
		Users: []config.User{{Name: "backup", Policy: "missing"}},
	}
	err := newReconciler(api, cfg).Run(context.Background())
	require.ErrorContains(t, err, `unknown policy "missing"`)
	assert.Zero(t, api.calls)
}

func TestRunDryRunDoesNotMutateGarage(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	cfg := config.Config{
		Users:    []config.User{{AccessKey: "GKONE", SecretKey: "secret", Policy: "backup"}},
		Policies: []config.Policy{{Name: "backup", Buckets: []config.PolicyBucket{{Name: "backups", Write: true}}}},
	}
	r := newReconciler(api, cfg)
	r.DryRun = true
	require.NoError(t, r.Run(context.Background()))
	assert.Zero(t, api.created)
	assert.Zero(t, api.imported)
	assert.Empty(t, api.buckets)
	assert.Empty(t, api.permissions)
}
