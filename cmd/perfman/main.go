package main

import (
	"os"

	"numa-perfman/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args))
}
