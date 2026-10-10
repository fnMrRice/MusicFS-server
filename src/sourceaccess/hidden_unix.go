//go:build linux || darwin

package sourceaccess

import "io/fs"

func hiddenAttribute(fs.FileInfo) bool { return false }
