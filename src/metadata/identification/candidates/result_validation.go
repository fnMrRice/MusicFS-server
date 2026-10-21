package candidates

import (
	"fmt"
	"reflect"
)

// ValidateNonFingerprintResult 验证 v1 哈希和标签证据的固定结果契约。
// 存储事务可据此复核候选，不依赖具体评分插件，也不执行音频算法。
func ValidateNonFingerprintResult(result Candidate, left, right Input) error {
	invalid := func() error { return fmt.Errorf("INVALID_ARGUMENT: invalid candidate evidence") }
	if ValidateInput(left) != nil || ValidateInput(right) != nil || left.Asset.SourceTrackID == right.Asset.SourceTrackID {
		return invalid()
	}
	if left.Asset.SourceTrackID > right.Asset.SourceTrackID {
		left, right = right, left
	}
	lh, err := CurrentHash(left)
	if err != nil {
		return err
	}
	rh, err := CurrentHash(right)
	if err != nil {
		return err
	}
	expected := Candidate{Left: left.Asset, Right: right.Asset, AlgorithmVersion: AlgorithmVersion, RequiresConfirmation: true, Reasons: []Reason{}}
	if lh != nil && rh != nil && lh.SHA512 == rh.SHA512 && lh.FileSizeBytes == rh.FileSizeBytes {
		expected.Score = 100
		expected.Reasons = append(expected.Reasons, Reason{Kind: "verifiedFileSHA512Equal", LeftEvidenceID: lh.EvidenceID, RightEvidenceID: rh.EvidenceID})
	} else {
		title, artist, delta := Metadata(left, right)
		if !title || !artist || delta == nil || *delta > 2000 {
			return invalid()
		}
		expected.Score = 35
		expected.Reasons = append(expected.Reasons, Reason{Kind: "metadataOnly"}, Reason{Kind: "titleEqual"}, Reason{Kind: "artistOverlap"}, Reason{Kind: "durationClose", DurationDifferenceMs: delta})
	}
	if !reflect.DeepEqual(expected, result) {
		return invalid()
	}
	return nil
}
