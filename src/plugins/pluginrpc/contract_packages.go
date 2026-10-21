package pluginrpc

import "strings"

// Source directories are not wire identities. Pin only the packages moved by
// the functional layout migration to their original persistence protocol names.
// Unknown packages retain their full identity; names and shapes are still
// fingerprinted. Keep these identities stable across future source moves.
var contractPackagePaths = map[string]string{
	"app/api":                                   "api",
	"app/bootstrap":                             "bootstrap",
	"core/auth":                                 "auth",
	"core/config":                               "config",
	"core/domain":                               "domain",
	"core/lifecycle":                            "lifecycle",
	"library/albums":                            "albums",
	"library/catalog":                           "catalog",
	"library/jobs":                              "jobs",
	"library/musicdirs":                         "musicdirs",
	"library/personal":                          "personal",
	"library/playlists":                         "playlists",
	"library/recommendations":                   "recommendations",
	"library/scanwatch":                         "scanwatch",
	"library/sourcedisplay":                     "sourcedisplay",
	"metadata/enrichment":                       "enrichment",
	"metadata/enrichmentworker":                 "enrichmentworker",
	"metadata/identification":                   "identification",
	"metadata/identification/candidates":        "identification/candidates",
	"metadata/identification/fingerprint":       "identification/fingerprint",
	"metadata/identification/fingerprintworker": "identification/fingerprintworker",
	"metadata/identification/hashworker":        "identification/hashworker",
	"metadata/matching":                         "matching",
	"metadata/writeback":                        "writeback",
	"metadata/writebackhost":                    "writebackhost",
	"playback/audio":                            "audio",
	"playback/audio/dspchain":                   "audio/dspchain",
	"playback/audio/dspworker":                  "audio/dspworker",
	"playback/processedcache":                   "processedcache",
	"playback/stream":                           "stream",
	"playback/timeline":                         "timeline",
	"plugins/accountstore":                      "accountstore",
	"plugins/onlinehost":                        "onlinehost",
	"plugins/pluginconfig":                      "pluginconfig",
	"plugins/plugincontrol":                     "plugincontrol",
	"plugins/plugindb":                          "plugindb",
	"plugins/pluginhost":                        "pluginhost",
	"plugins/pluginid":                          "pluginid",
	"plugins/pluginrpc":                         "pluginrpc",
	"plugins/sourceaccess":                      "sourceaccess",
	"storage/backup":                            "backup",
	"storage/mediastore":                        "mediastore",
	"storage/mediastore/content":                "mediastore/content",
}

func contractPackagePath(path string) string {
	const source = "github.com/fnMrRice/MusicFS-server/src/"
	if relative, ok := strings.CutPrefix(path, source); ok {
		if original, moved := contractPackagePaths[relative]; moved {
			return source + original
		}
	}
	return path
}
