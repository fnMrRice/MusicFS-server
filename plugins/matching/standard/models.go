// Package standard 提供在宿主进程内执行的默认候选评分算法。
package standard

import "github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"

type Input = candidates.Input
type Candidate = candidates.Candidate
type Reason = candidates.Reason
type FingerprintComparison = candidates.FingerprintComparison

const AlgorithmVersion = candidates.AlgorithmVersion
const MaxInputs = candidates.MaxInputs
const MaxResults = candidates.MaxResults
const MaxPopcounts = candidates.MaxPopcounts

type Plugin struct{}

var _ candidates.Scorer = Plugin{}
