package garage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientUsesAdminAPIAndBearerToken(t *testing.T) {
	t.Parallel()

	t.Run("Uses admin API and Bearer token", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v2/GetBucketInfo", r.URL.Path)
			assert.Equal(t, "backups", r.URL.Query().Get("globalAlias"))
			assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"bucket-id"}`))
		}))
		defer server.Close()

		adminURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		client := NewClient(adminURL, "token", time.Second)
		bucket, err := client.GetBucket(context.Background(), "backups")

		require.NoError(t, err)
		assert.Equal(t, "bucket-id", bucket.ID)
	})

	t.Run("UpdateKey builds typed request", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v2/UpdateKey", r.URL.Path)
			assert.Equal(t, "key-id", r.URL.Query().Get("id"))

			var body updateKeyRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "backup", body.Name)
			assert.True(t, body.Allow.CreateBucket)
		}))
		defer server.Close()

		adminURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		client := NewClient(adminURL, "", time.Second)

		require.NoError(t, client.UpdateKey(context.Background(), "key-id", "backup", true))
	})
}

func TestClientLookupsHandleNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not JSON"))
	}))
	t.Cleanup(server.Close)

	adminURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := NewClient(adminURL, "", time.Second)

	t.Run("GetKey", func(t *testing.T) {
		key, err := client.GetKey(context.Background(), "missing-key")
		require.NoError(t, err)
		assert.Nil(t, key)
	})

	t.Run("GetBucket", func(t *testing.T) {
		bucket, err := client.GetBucket(context.Background(), "missing-bucket")
		require.NoError(t, err)
		assert.Nil(t, bucket)
	})
}

func TestClientListKeysPropagatesHTTPError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		retryable bool
	}{
		{name: "not found", status: http.StatusNotFound},
		{name: "forbidden", status: http.StatusForbidden},
		{name: "rate limited", status: http.StatusTooManyRequests, retryable: true},
		{name: "server error", status: http.StatusInternalServerError, retryable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v2/ListKeys", r.URL.Path)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("not JSON"))
			}))
			t.Cleanup(server.Close)

			adminURL, err := url.Parse(server.URL)
			require.NoError(t, err)
			client := NewClient(adminURL, "", time.Second)

			keys, err := client.ListKeys(context.Background())
			assert.Nil(t, keys)

			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, "/v2/ListKeys", apiErr.Endpoint)
			assert.Equal(t, tt.status, apiErr.Status)
			assert.Equal(t, tt.retryable, apiErr.Retryable())
		})
	}
}

func TestAPIErrorRetryable(t *testing.T) {
	t.Parallel()

	assert.True(t, (&APIError{Status: 0}).Retryable())
}

func TestClientLookupsPropagateOtherErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	adminURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := NewClient(adminURL, "", time.Second)

	key, err := client.GetKey(context.Background(), "key")
	assert.Nil(t, key)
	var keyErr *APIError
	require.ErrorAs(t, err, &keyErr)
	assert.Equal(t, http.StatusInternalServerError, keyErr.Status)

	bucket, err := client.GetBucket(context.Background(), "bucket")
	assert.Nil(t, bucket)
	var bucketErr *APIError
	require.ErrorAs(t, err, &bucketErr)
	assert.Equal(t, http.StatusInternalServerError, bucketErr.Status)
}

func TestClientCreateKeyDoesNotIgnoreNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	adminURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := NewClient(adminURL, "", time.Second)

	_, err = client.CreateKey(context.Background(), "key")
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Status)
}

func TestClientRequestReturnsRawHTTPResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v2/ListKeys", r.URL.Path)
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not JSON"))
	}))
	t.Cleanup(server.Close)

	adminURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := NewClient(adminURL, "token", time.Second)

	status, body, err := client.get(context.Background(), client.endpoint("ListKeys", nil))
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, []byte("not JSON"), body)
}

func TestClientSuccessStatusesAndJSONDecoding(t *testing.T) {
	t.Parallel()

	t.Run("GetKey requires HTTP 200", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"accessKeyId":"key-id"}`))
		}))
		t.Cleanup(server.Close)

		adminURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		client := NewClient(adminURL, "", time.Second)

		key, err := client.GetKey(context.Background(), "key-id")
		assert.Nil(t, key)
		var apiErr *APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusCreated, apiErr.Status)
	})

	t.Run("CreateBucket accepts HTTP 201 and decodes its body", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/v2/CreateBucket", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"bucket-id"}`))
		}))
		t.Cleanup(server.Close)

		adminURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		client := NewClient(adminURL, "", time.Second)

		bucket, err := client.CreateBucket(context.Background(), "backups")
		require.NoError(t, err)
		assert.Equal(t, "bucket-id", bucket.ID)
	})

	t.Run("UpdateKey accepts HTTP 204 with no body", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(server.Close)

		adminURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		client := NewClient(adminURL, "", time.Second)

		require.NoError(t, client.UpdateKey(context.Background(), "id", "name", true))
	})

	t.Run("successful response with malformed JSON returns decode error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not JSON"))
		}))
		t.Cleanup(server.Close)

		adminURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		client := NewClient(adminURL, "", time.Second)

		keys, err := client.ListKeys(context.Background())
		assert.Nil(t, keys)
		require.ErrorContains(t, err, "decode /v2/ListKeys response")
	})
}

func TestClientPostOperationsPropagateHTTPError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		operation func(*Client) error
	}{
		{
			name: "import key", path: "/v2/ImportKey",
			operation: func(c *Client) error {
				_, err := c.ImportKey(context.Background(), "id", "secret", "name")
				return err
			},
		},
		{
			name: "create key", path: "/v2/CreateKey",
			operation: func(c *Client) error {
				_, err := c.CreateKey(context.Background(), "name")
				return err
			},
		},
		{
			name: "update key", path: "/v2/UpdateKey",
			operation: func(c *Client) error {
				return c.UpdateKey(context.Background(), "id", "name", false)
			},
		},
		{
			name: "create bucket", path: "/v2/CreateBucket",
			operation: func(c *Client) error {
				_, err := c.CreateBucket(context.Background(), "name")
				return err
			},
		},
		{
			name: "allow bucket key", path: "/v2/AllowBucketKey",
			operation: func(c *Client) error {
				return c.AllowBucketKey(context.Background(), "bucket", "key", Permissions{Read: true})
			},
		},
		{
			name: "deny bucket key", path: "/v2/DenyBucketKey",
			operation: func(c *Client) error {
				return c.DenyBucketKey(context.Background(), "bucket", "key", Permissions{Read: true})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.path, r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("not JSON"))
			}))
			t.Cleanup(server.Close)

			adminURL, err := url.Parse(server.URL)
			require.NoError(t, err)
			client := NewClient(adminURL, "", time.Second)

			var apiErr *APIError
			require.ErrorAs(t, tt.operation(client), &apiErr)
			assert.Equal(t, tt.path, apiErr.Endpoint)
			assert.Equal(t, http.StatusTooManyRequests, apiErr.Status)
			assert.True(t, apiErr.Retryable())
		})
	}
}
