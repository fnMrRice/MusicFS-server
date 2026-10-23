// Package chromaprint 提供在宿主进程内调用的内置指纹分析插件。
package chromaprint

import (
	"time"

	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
)

type Config struct {
	FpcalcPath, FpcalcSHA256, DecoderSHA256, DecoderVersion string
	MaxConcurrent                                           int
	Timeout                                                 time.Duration
}

type Normalizer = fingerprint.Normalizer
type Identity = fingerprint.Identity
type ToolIdentity = fingerprint.ToolIdentity
type Request = fingerprint.Request
type Detail = fingerprint.Detail

const AlgorithmVersion = "1.6.1"
const QualityRevision = fingerprint.QualityRevision
const MaxDetailBytes = fingerprint.MaxDetailBytes
