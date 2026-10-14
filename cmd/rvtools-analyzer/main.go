package main

import (
	"fmt"
	"os"

	"go.uber.org/zap"
)

var (
	version   = "v0.0.0"
	gitCommit = "unknown"
)

func main() {
	root := NewRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = zap.L().Sync()
}
