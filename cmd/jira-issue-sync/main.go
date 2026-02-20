package main

import (
	"os"

	"github.com/pat/jira-issue-sync/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
