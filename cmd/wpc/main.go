package main

import (
	"fmt"
	"os"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/cli"
)

// Release builds supply these values through the Go linker.
var (
	version = "dev"
	commit  = ""
)

func main() {
	cmd := cli.NewRootCommand(version, commit)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
