package main

import (
	"fmt"
	"os"

	"github.com/fnMrRice/MusicFS-server/src/bootstrap"
)

func main() {
	if err := bootstrap.Run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
