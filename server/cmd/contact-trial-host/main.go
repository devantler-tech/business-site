// Command contact-trial-host is the explicit, disposable private-contact host.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/devantler-tech/business-site/server/internal/contacttrialhost"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := contacttrialhost.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
