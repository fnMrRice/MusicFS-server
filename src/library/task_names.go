package library

import "github.com/fnMrRice/MusicFS-server/src/tasks"

// TaskNames 仅返回已有公开显示名，任务列表不获得目录路径或来源访问能力。
func (s *Service) TaskNames(owner, provider string, ids []string) (string, []tasks.Root) {
	runtime, exists := s.Runtime(provider)
	if !exists || runtime.Owner != owner {
		return "", nil
	}
	roots := make([]tasks.Root, 0, len(ids))
	for _, id := range ids {
		name := runtime.RootNames[id]
		if name == "" {
			name = id
		}
		roots = append(roots, tasks.Root{ID: id, Name: name})
	}
	return runtime.Name, roots
}
