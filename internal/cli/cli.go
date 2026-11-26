// Package flags parses garage-bootstrap command-line flags and environment.
package cli

import (
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/containeroo/tinyflags"
)

// Flags configures the process.
type Flags struct {
	// AdminURL is the Garage Admin API URL.
	AdminURL *url.URL
	// ConfigPath is the YAML configuration path.
	ConfigPath string
	// AdminToken authenticates Admin API requests.
	AdminToken string
	// RequestTimeout bounds an Admin API request.
	RequestTimeout time.Duration
	// ListenAddress is the address for the health HTTP server.
	ListenAddress string
	// DryRun logs intended changes.
	DryRun bool
}

// Parse parses CLI arguments and GARAGE_BOOTSTRAP_* environment variables.
func Parse(args []string, version string) (flags Flags, err error) {
	tf := tinyflags.NewFlagSet("garage-bootstrap", tinyflags.ContinueOnError)

	tf.EnvPrefix("GARAGE_BOOTSTRAP_")
	tf.Description("Reconcile Garage resources once and serve health checks until shutdown.")
	tf.Version(version)

	tf.URLVar(&flags.AdminURL, "admin-url", &url.URL{}, "Garage Admin API base URL").
		Required().
		Validate(validAdminURL).
		Value()

	tf.StringVar(&flags.ConfigPath, "config", "", "path to YAML bootstrap configuration").
		Required().
		Value()

	tf.StringVar(&flags.AdminToken, "admin-token", "", "Garage Admin API bearer token").
		Value()

	tf.DurationVar(&flags.RequestTimeout, "request-timeout", 10*time.Second, "timeout for one Admin API request").
		Validate(tinyflags.Positive[time.Duration]()).
		Value()

	listenAddr := tf.TCPAddr("listen-address", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}, "address for the health HTTP server").
		Value()

	tf.BoolVar(&flags.DryRun, "dry-run", false, "log changes without applying them")

	if err := tf.Parse(args); err != nil {
		return Flags{}, err
	}

	flags.ListenAddress = (*listenAddr).String()

	return flags, nil
}

// validAdminURL validates an Admin API endpoint.
func validAdminURL(value *url.URL) error {
	if (value.Scheme != "http" && value.Scheme != "https") || value.Host == "" {
		return fmt.Errorf("must be an absolute HTTP(S) URL")
	}
	return nil
}
