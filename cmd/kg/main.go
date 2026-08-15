package main

import (
	"context"
	"os"

	"kg-acme/internal/cli"
)

func main() { os.Exit((cli.Runner{}).Run(context.Background(), os.Args[1:])) }
