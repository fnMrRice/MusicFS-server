//go:build darwin

package sourceaccess

import (
	"fmt"
	"os"
)

func protectStableFile(*os.File) (stableProtection, error) {
	return stableProtection{}, fmt.Errorf("UNSUPPORTED: stable source reads are not implemented on this platform")
}
