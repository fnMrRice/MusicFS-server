package sourceaccess

import (
	"fmt"
	"os"
	"path/filepath"
)

// Open relative to the deployment boundary so links and junctions cannot
// redirect a selected directory outside it between validation and access.
func openConfiguredRoot(cfg RootConfig) (*os.Root, error) {
	if cfg.Boundary == "" {
		return os.OpenRoot(cfg.Path)
	}
	rel, err := filepath.Rel(cfg.Boundary, cfg.Path)
	if err != nil || rel != "." && !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("ACCESS_DENIED: root is outside boundary")
	}
	parent, err := os.OpenRoot(cfg.Boundary)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return parent.OpenRoot(rel)
}
