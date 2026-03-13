package main

import (
	"fmt"
	"os"

	"auto/internal/runtime"
)

func main() {
	if err := runtime.RunCLI(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
