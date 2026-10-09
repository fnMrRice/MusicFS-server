package main

import (
	"fmt"
	"github.com/fnMrRice/MusicFS-server/src/bootstrap"
	"os"
)

func main() {
	if err := bootstrap.Run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
