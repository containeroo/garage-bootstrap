// Package app wires process configuration to the Garage reconciler.
package app

import (
	"context"
	"fmt"
	"io"

	appkitlogging "github.com/containeroo/appkit/logging"
	"github.com/containeroo/httpkit/grace"
	"github.com/containeroo/tinyflags"

	"github.com/gi8lino/garage-bootstrap/internal/config"
	"github.com/gi8lino/garage-bootstrap/internal/flags"
	"github.com/gi8lino/garage-bootstrap/internal/garage"
	"github.com/gi8lino/garage-bootstrap/internal/reconcile"
	"github.com/gi8lino/garage-bootstrap/internal/server"
)

// Run reconciles Garage once, then serves health checks until shutdown.
func Run(
	ctx context.Context,
	version string,
	args []string,
	stdout, stderr io.Writer,
) error {
	options, err := flags.Parse(args, version)
	if err != nil {
		if tinyflags.IsHelpRequested(err) || tinyflags.IsVersionRequested(err) {
			_, _ = fmt.Fprint(stdout, err)
			return nil
		}
		fmt.Fprintln(stderr, err) // nolint:errcheck
		return err
	}

	logger := appkitlogging.Setup(appkitlogging.LogFormatJSON, false, stderr)
	setupLogger := logger.With("component", "setup")

	cfg, err := config.Load(options.ConfigPath)
	if err != nil {
		setupLogger.Error("failed to load configuration", "error", err)
		return err
	}

	client := garage.NewClient(options.AdminURL, options.AdminToken, options.RequestTimeout)

	// Use one process-lifetime context for reconciliation and graceful shutdown.
	ctx, stop := grace.SignalContext(ctx)
	defer stop()

	reconcile := reconcile.Reconciler{
		API:    client,
		Config: cfg,
		Logger: logger,
		DryRun: options.DryRun,
	}
	if err := reconcile.Run(ctx); err != nil {
		setupLogger.Error("failed to reconcile configuration", "error", err)
		return err
	}

	logger.Info("reconciliation complete")

	// Construct both HTTP handlers and run their listeners until shutdown.
	handler := server.ManagementHandler()
	if err := grace.Run(ctx, options.ListenAddress, handler, logger); err != nil {
		setupLogger.Error("failed to serve management API", "error", err)
		return err
	}

	return nil
}
