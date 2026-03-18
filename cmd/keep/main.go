package main

import (
	"os"

	"github.com/majorcontext/keep/cmd/keep/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
