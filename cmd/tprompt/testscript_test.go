package main

import (
	"os"
	"testing"

	"github.com/hsadler/tprompt/internal/app"
	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"tprompt": func() {
			os.Exit(app.RunCLI(os.Args[1:], os.Stdout, os.Stderr, os.Stdin))
		},
	})
}

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
	})
}
