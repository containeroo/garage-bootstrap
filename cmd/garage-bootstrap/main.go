// garage-bootstrap reconciles Garage administrative resources declared in YAML.
package main

import (
	"context"
	"os"

	"github.com/gi8lino/garage-bootstrap/internal/app"
)

var (
	Version string = "dev"
	Commit  string = "none"
)

func main() {
	if err := app.Run(
		context.Background(),
		Version,
		os.Args[1:],
		os.Stdout,
		os.Stderr,
	); err != nil {
		os.Exit(1)
	}
}
