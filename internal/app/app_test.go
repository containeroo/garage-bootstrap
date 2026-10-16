package app

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gi8lino/garage-bootstrap/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagementHandler(t *testing.T) {
	t.Parallel()
	handler := server.ManagementHandler()
	for _, test := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{http.MethodGet, "/missing", http.StatusNotFound},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
		assert.Equal(t, test.want, recorder.Code)
		if test.want == http.StatusOK {
			assert.Equal(t, "ok\n", recorder.Body.String())
		}
	}
}

func TestRunReconcilesOnceBeforeServingHealth(t *testing.T) {
	var calls atomic.Int32
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/v2/ListKeys", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[ { "accessKeyId": "key-id", "name": "backup", "permissions": { "createBucket": false } } ]`))
	}))
	defer admin.Close()

	path := filepath.Join(t.TempDir(), "bootstrap.yaml")
	require.NoError(t, os.WriteFile(path, []byte("users:\n  - name: backup\n    policy: none\npolicies:\n  - name: none\n"), 0o600))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stderr := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "test", []string{
			"--admin-url", admin.URL,
			"--config", path,
			"--listen-address", address,
		}, &bytes.Buffer{}, stderr)
	}()

	healthURL := "http://" + address + "/healthz"
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for {
		response, err := client.Get(healthURL)
		if err == nil {
			require.Equal(t, http.StatusOK, response.StatusCode)
			_ = response.Body.Close()
			break
		}
		select {
		case runErr := <-done:
			t.Fatalf("server exited before becoming healthy: %v", runErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("health endpoint did not become available: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	assert.EqualValues(t, 1, calls.Load())
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shut down")
	}
	assert.Contains(t, stderr.String(), "starting garage bootstrap")
	assert.Contains(t, stderr.String(), "configuration loaded")
	assert.Contains(t, stderr.String(), "starting reconciliation")
	assert.Contains(t, stderr.String(), "starting server")
}

func TestRunFailsBeforeStartingHealthServer(t *testing.T) {
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer admin.Close()

	path := filepath.Join(t.TempDir(), "bootstrap.yaml")
	require.NoError(t, os.WriteFile(path, []byte("users:\n  - name: backup\n    policy: none\npolicies:\n  - name: none\n"), 0o600))

	err := Run(context.Background(), "test", []string{
		"--admin-url", admin.URL,
		"--config", path,
		"--listen-address", "127.0.0.1:0",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "HTTP 401")
}
