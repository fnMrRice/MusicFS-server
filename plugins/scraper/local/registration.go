package scraperlocal

import "github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"

func init() {
	if err := enrichment.Register(Descriptor(), func(c enrichment.HostConfig) (enrichment.Host, error) {
		return NewHost(c)
	}); err != nil {
		panic(err)
	}
}
