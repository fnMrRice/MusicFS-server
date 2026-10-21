//go:build !windows

package musicdirs

// Discover leaves drive discovery disabled on platforms without Windows drives.
func Discover() []string {
	return nil
}
