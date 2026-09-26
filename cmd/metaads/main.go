package main

import (
	"os"

	"github.com/wir-drei-digital/meta-ads-cli/internal/cli"
)

func main() { os.Exit(cli.Execute(os.Args[1:])) }
