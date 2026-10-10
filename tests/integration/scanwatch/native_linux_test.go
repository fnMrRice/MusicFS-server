//go:build linux

package scanwatch_test

import "os"

func testOpenHandle(path string) (*os.File, error) { return os.Open(path) }
