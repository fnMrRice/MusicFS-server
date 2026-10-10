package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Only reviewed user documentation and contracts enter installation bundles.
// Even --allow-dirty never includes ignored or untracked documentation files.
var userDocuments = map[string]bool{
	"deployment.md": true, "backup.md": true, "running.md": true,
	"configuration.md": true, "ncm.md": true, "online.md": true,
	"fingerprinting.md": true, "matching.md": true, "media.md": true,
	"media-storage.md": true, "albums.md": true, "derived-media.md": true,
	"metadata.md": true, "writeback.md": true, "recovery.md": true,
	"recommendations.md": true, "audio-processing.md": true,
	"platform-capabilities.md": true,
	"dependencies.md":          true, "lifecycle.md": true, "remote-playlists.md": true,
	"playlists.md": true, "processed-cache.md": true, "identification.md": true,
	"scanning.md": true,
}

var deploymentFiles = map[string]bool{
	"deploy/Dockerfile": true, "deploy/compose.example.yaml": true,
	"deploy/server.container.example.yaml": true, "deploy/nginx.example.conf": true,
}

func (b builder) documents() error {
	for _, name := range []string{"LICENSE", "go.mod", "go.sum", "configs/server.schema.json"} {
		if err := copyRegular(filepath.Join(b.root, name), filepath.Join(b.out, name), 4<<20); err != nil {
			return err
		}
	}
	files, err := b.command("git", "ls-files", "-z", "--", "docs", "deploy", "scripts/scan.ps1", "src/identification/fingerprint/REFERENCES.md")
	if err != nil {
		return err
	}
	for _, name := range strings.Split(string(files), "\x00") {
		if name == "" || !releaseDocument(name) {
			continue
		}
		if err = b.checkedDocument(name); err != nil {
			return err
		}
		if err = copyRegular(filepath.Join(b.root, filepath.FromSlash(name)), filepath.Join(b.out, filepath.FromSlash(name)), 8<<20); err != nil {
			return err
		}
	}
	readme := "# MusicFS 私有安装包\n\n此目录包含 Server 与独立插件程序，不包含音乐、账号凭据或开发数据库。\n\n" +
		"从 [安装与部署](docs/deployment.md) 开始；配置示例为 configs/server.example.yaml，全部插件默认关闭。\n\n" +
		"- [运行与扫描](docs/running.md)，含 [PowerShell 扫描脚本](scripts/scan.ps1)\n- [配置说明](docs/configuration.md)\n- [备份与恢复](docs/backup.md)\n" +
		"- [NCM 格式参考及边界](docs/ncm.md)\n- [在线平台参考及边界](docs/platform-capabilities.md)\n\n" +
		"release.json 记录版本、构建平台与依赖许可证；SHA256SUMS 校验文件一致性。Go 工具链许可证位于 licenses/go。\n\n" +
		"该制品仅供私有安装验证，不自动上传或公开分发。开发实施和私人验收记录不随包交付；用户说明中指向 validation/implementation 的历史依据，请在对应 revision 的私有源仓库查阅。\n"
	return os.WriteFile(filepath.Join(b.out, "README.md"), []byte(readme), 0600)
}

func releaseDocument(name string) bool {
	if deploymentFiles[name] || name == "scripts/scan.ps1" || name == "src/identification/fingerprint/REFERENCES.md" {
		return true
	}
	if strings.HasPrefix(name, "docs/contracts/") {
		switch filepath.Ext(name) {
		case ".md", ".json", ".yaml":
			return true
		}
	}
	return strings.HasPrefix(name, "docs/") && !strings.Contains(strings.TrimPrefix(name, "docs/"), "/") && userDocuments[strings.TrimPrefix(name, "docs/")]
}

func (b builder) checkedDocument(name string) error {
	if !filepath.IsLocal(filepath.FromSlash(name)) {
		return errors.New("documentation path is outside the repository")
	}
	path := b.root
	for _, part := range strings.Split(name, "/") {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("documentation links are not supported")
		}
	}
	return nil
}
