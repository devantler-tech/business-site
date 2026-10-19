// Command contact-trial-report analyzes explicitly supplied private timing data.
// It is not linked into or invoked by the portal runtime.
package main

import (
	"os"

	"github.com/devantler-tech/business-site/server/internal/contacttrial"
)

func main() { os.Exit(contacttrial.Execute(os.Args[1:], os.Stderr)) }
