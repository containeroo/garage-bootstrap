package server

import (
	"io"
	"net/http"

	apphttpserver "github.com/containeroo/appkit/httpserver"
)

// ManagementHandler serves health checks without exposing an application API.
func ManagementHandler() http.Handler {
	return apphttpserver.NewManagement(apphttpserver.ManagementRoutes{
		Health: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok\n")
		}),
	})
}
