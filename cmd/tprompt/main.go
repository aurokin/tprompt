package main

import (
	"io"
	"os"

	"github.com/hsadler/tprompt/internal/app"
)

func run(args []string, stdout, stderr io.Writer) int {
	cmd := app.NewRootCmd()
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
