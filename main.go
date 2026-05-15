package main

import (
	"fmt"
	"os"

	"github.com/rossturk/rowner/cmd"
)

func main() {
	if err := cmd.Root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "rowner:", err)
		os.Exit(1)
	}
}
