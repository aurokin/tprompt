package main

import (
	"os"

	"github.com/hsadler/tprompt/internal/app"
)

func main() {
	os.Exit(app.RunCLI(os.Args[1:], os.Stdout, os.Stderr, os.Stdin))
}
