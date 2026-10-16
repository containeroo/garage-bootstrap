// Package garage implements the small Garage v2.4.1 Admin API subset needed by
// garage-bootstrap. It intentionally does not use Garage's preview Go SDK.
package garage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiPrefix = "/v2/"

// Client calls Garage's v2.4.1 Admin API.
type Client struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

// APIError contains a safe, structured description of an Admin API failure.
type APIError struct {
	// Endpoint is the failed API path.
	Endpoint string
	// Status is the HTTP status.
	Status int
	// Message is a safe error detail.
	Message string
}

// Error returns the safe API failure detail.
func (e *APIError) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("Garage Admin API %s: %s", e.Endpoint, e.Message)
	}
	return fmt.Sprintf("Garage Admin API %s: HTTP %d: %s", e.Endpoint, e.Status, e.Message)
}

// Retryable reports whether retrying this error can be useful.
func (e *APIError) Retryable() bool {
	return e.Status == 0 || e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// NewClient creates a client from a URL already validated by flags.Parse.
func NewClient(baseURL *url.URL, token string, timeout time.Duration) *Client {
	base := *baseURL
	base.Path = strings.TrimRight(base.Path, "/") + "/"
	return &Client{
		baseURL: &base,
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}
}

// Key is a Garage access key returned by the Admin API.
type Key struct {
	// AccessKeyID is the S3 key ID.
	AccessKeyID string `json:"accessKeyId"`
	// Name is the key name.
	Name string `json:"name"`
	// Permissions are key permissions.
	Permissions KeyPermissions `json:"permissions"`
}

// KeyPermissions are administrative permissions on an access key.
type KeyPermissions struct {
	// CreateBucket permits bucket creation.
	CreateBucket bool `json:"createBucket"`
}

// Bucket is a Garage bucket returned by the Admin API.
type Bucket struct {
	// ID is the Garage bucket ID.
	ID string `json:"id"`
}

// Permissions are S3 permissions for one key on one bucket.
type Permissions struct {
	// Read permits reads.
	Read bool `json:"read"`
	// Write permits writes.
	Write bool `json:"write"`
	// Owner permits owner operations.
	Owner bool `json:"owner"`
}

// GetKey returns the key identified by ID. It returns (nil, nil) for 404.
func (c *Client) GetKey(ctx context.Context, id string) (*Key, error) {
	endpoint := c.endpoint("GetKeyInfo", url.Values{"id": {id}})

	status, data, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, unexpectedStatus(endpoint, status)
	}

	var key Key
	if err := decodeResponse(endpoint, data, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// ListKeys returns all Garage access keys.
func (c *Client) ListKeys(ctx context.Context) ([]Key, error) {
	endpoint := c.endpoint("ListKeys", nil)

	status, data, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, unexpectedStatus(endpoint, status)
	}

	var keys []Key
	if err := decodeResponse(endpoint, data, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// ImportKey imports a caller-provided access key into Garage.
func (c *Client) ImportKey(ctx context.Context, id, secret, name string) (*Key, error) {
	endpoint := c.endpoint("ImportKey", nil)
	body := importKeyRequest{AccessKeyID: id, SecretAccessKey: secret, Name: name}

	status, data, err := c.post(ctx, endpoint, body)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, unexpectedStatus(endpoint, status)
	}

	var key Key
	if err := decodeResponse(endpoint, data, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// CreateKey creates a new Garage-generated access key.
func (c *Client) CreateKey(ctx context.Context, name string) (*Key, error) {
	endpoint := c.endpoint("CreateKey", nil)
	body := createKeyRequest{Name: name}

	status, data, err := c.post(ctx, endpoint, body)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, unexpectedStatus(endpoint, status)
	}

	var key Key
	if err := decodeResponse(endpoint, data, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// UpdateKey sets the managed key's name and create-bucket permission.
func (c *Client) UpdateKey(ctx context.Context, id, name string, allowCreateBucket bool) error {
	endpoint := c.endpoint("UpdateKey", url.Values{"id": {id}})
	body := updateKeyRequest{
		Name:  name,
		Allow: KeyPermissions{CreateBucket: allowCreateBucket},
	}

	status, _, err := c.post(ctx, endpoint, body)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return unexpectedStatus(endpoint, status)
	}
	return nil
}

// importKeyRequest is an ImportKey request.
type importKeyRequest struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	Name            string `json:"name"`
}

// createKeyRequest is a CreateKey request.
type createKeyRequest struct {
	Name string `json:"name"`
}

// updateKeyRequest is an UpdateKey request.
type updateKeyRequest struct {
	// Name is the key name.
	Name string `json:"name"`
	// Allow defines allowed permissions.
	Allow KeyPermissions `json:"allow"`
}

// GetBucket returns the bucket identified by global alias. It returns (nil, nil) for 404.
func (c *Client) GetBucket(ctx context.Context, alias string) (*Bucket, error) {
	endpoint := c.endpoint("GetBucketInfo", url.Values{"globalAlias": {alias}})

	status, data, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, unexpectedStatus(endpoint, status)
	}

	var bucket Bucket
	if err := decodeResponse(endpoint, data, &bucket); err != nil {
		return nil, err
	}
	return &bucket, nil
}

// CreateBucket creates a bucket with a global alias.
func (c *Client) CreateBucket(ctx context.Context, alias string) (*Bucket, error) {
	endpoint := c.endpoint("CreateBucket", nil)
	body := createBucketRequest{GlobalAlias: alias}

	status, data, err := c.post(ctx, endpoint, body)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, unexpectedStatus(endpoint, status)
	}

	var bucket Bucket
	if err := decodeResponse(endpoint, data, &bucket); err != nil {
		return nil, err
	}
	return &bucket, nil
}

// AllowBucketKey enables the supplied permissions. False fields are unchanged by Garage.
func (c *Client) AllowBucketKey(ctx context.Context, bucketID, keyID string, permissions Permissions) error {
	endpoint := c.endpoint("AllowBucketKey", nil)
	body := permissionRequest(bucketID, keyID, permissions)

	status, _, err := c.post(ctx, endpoint, body)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return unexpectedStatus(endpoint, status)
	}
	return nil
}

// createBucketRequest is a CreateBucket request.
type createBucketRequest struct {
	GlobalAlias string `json:"globalAlias"`
}

// permissionRequest builds a permission request.
func permissionRequest(bucketID, keyID string, permissions Permissions) bucketKeyRequest {
	return bucketKeyRequest{BucketID: bucketID, AccessKeyID: keyID, Permissions: permissions}
}

// bucketKeyRequest changes bucket key permissions.
type bucketKeyRequest struct {
	BucketID    string      `json:"bucketId"`
	AccessKeyID string      `json:"accessKeyId"`
	Permissions Permissions `json:"permissions"`
}

// endpoint returns an Admin API URL.
func (c *Client) endpoint(name string, query url.Values) *url.URL {
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: apiPrefix + name})
	endpoint.RawQuery = query.Encode()
	return endpoint
}

// get performs an Admin API GET.
func (c *Client) get(ctx context.Context, endpoint *url.URL) (int, []byte, error) {
	return c.request(ctx, http.MethodGet, endpoint, nil)
}

// post performs an Admin API POST.
func (c *Client) post(ctx context.Context, endpoint *url.URL, body any) (int, []byte, error) {
	return c.request(ctx, http.MethodPost, endpoint, body)
}

// unexpectedStatus reports an HTTP status not accepted by an API operation.
func unexpectedStatus(endpoint *url.URL, status int) error {
	return &APIError{
		Endpoint: endpoint.Path,
		Status:   status,
		Message:  "unexpected response",
	}
}

// decodeResponse decodes a successful JSON response when a body is present.
func decodeResponse(endpoint *url.URL, data []byte, out any) error {
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s response: %w", endpoint.Path, err)
	}
	return nil
}

// request executes an Admin API request and returns the raw HTTP response.
// Its caller decides which status codes are acceptable.
func (c *Client) request(ctx context.Context, method string, endpoint *url.URL, body any) (int, []byte, error) {
	payload, err := encodeBody(endpoint, body)
	if err != nil {
		return 0, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return 0, nil, fmt.Errorf("create %s request: %w", endpoint.Path, err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.http.Do(req)
	if err != nil {
		return 0, nil, &APIError{Endpoint: endpoint.Path, Message: err.Error()}
	}
	defer response.Body.Close() // nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("read %s response: %w", endpoint.Path, err)
	}
	return response.StatusCode, data, nil
}

// encodeBody marshals a request body.
func encodeBody(endpoint *url.URL, body any) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode %s request: %w", endpoint.Path, err)
	}
	return payload, nil
}
