// Package unified composes the official online adapters into one executable.
package unified

import (
	"fmt"
	"strings"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

// Select fixes one platform before opening the protocol channel. Neither the
// handshake nor a later business request can switch a running worker's platform.
func Select(args []string) (provider.Plugin, error) {
	var platform string
	switch {
	case len(args) == 2 && args[0] == "--plugin":
		platform = args[1]
	case len(args) == 1 && strings.HasPrefix(args[0], "--plugin="):
		platform = strings.TrimPrefix(args[0], "--plugin=")
	default:
		return provider.Plugin{}, fmt.Errorf("Exactly one --plugin netease|qqmusic|applemusic argument is required")
	}
	switch platform {
	case online.Netease, online.QQMusic, online.AppleMusic:
		return onlineplugins.Plugin(platform, onlineplugins.Options{}), nil
	default:
		return provider.Plugin{}, fmt.Errorf("Unsupported online plugin platform")
	}
}
