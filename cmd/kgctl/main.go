package main

import (
	"context"
	"os"

	"kg-acme/internal/cli"
)

func main() { os.Exit((cli.ControlRunner{}).Run(context.Background(), os.Args[1:])) }
