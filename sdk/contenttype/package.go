package contenttype

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// PackageEntry 是宿主授权快照中的结构摘要，不授予文件访问权。
type PackageEntry struct{ Path, Kind string }

// PackageOrganizer 只决定显示顺序，不能新增路径、改写来源身份或合并文件。
type PackageOrganizer interface {
	OrderPackage([]PackageEntry) ([]string, error)
}

// DirectoryOrganizer 保留原始目录，按路径段中的数字自然排序，不推断内容版本。
type DirectoryOrganizer struct{}

func (DirectoryOrganizer) OrderPackage(entries []PackageEntry) ([]string, error) {
	if len(entries) == 0 || len(entries) > 5000 {
		return nil, fmt.Errorf("Invalid package entry count")
	}
	paths := make([]string, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.Path == "" || seen[entry.Path] {
			return nil, fmt.Errorf("Invalid package path")
		}
		seen[entry.Path] = true
		paths = append(paths, entry.Path)
	}
	slices.SortFunc(paths, func(a, b string) int {
		if a == b {
			return 0
		}
		if a == "." {
			return -1
		}
		if b == "." {
			return 1
		}
		as, bs := strings.Split(a, "/"), strings.Split(b, "/")
		for i := 0; i < min(len(as), len(bs)); i++ {
			if order := naturalCompare(as[i], bs[i]); order != 0 {
				return order
			}
		}
		return cmp.Compare(len(as), len(bs))
	})
	return paths, nil
}

func naturalCompare(a, b string) int {
	left, right := strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		if left[i] >= '0' && left[i] <= '9' && right[j] >= '0' && right[j] <= '9' {
			x, y := i, j
			for i < len(left) && left[i] >= '0' && left[i] <= '9' {
				i++
			}
			for j < len(right) && right[j] >= '0' && right[j] <= '9' {
				j++
			}
			ln, rn := strings.TrimLeft(left[x:i], "0"), strings.TrimLeft(right[y:j], "0")
			if order := cmp.Compare(len(ln), len(rn)); order != 0 {
				return order
			}
			if order := strings.Compare(ln, rn); order != 0 {
				return order
			}
			continue
		}
		if left[i] != right[j] {
			return cmp.Compare(left[i], right[j])
		}
		i++
		j++
	}
	if order := cmp.Compare(len(left)-i, len(right)-j); order != 0 {
		return order
	}
	return strings.Compare(a, b)
}
