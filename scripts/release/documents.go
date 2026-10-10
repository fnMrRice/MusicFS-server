package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Installation bundles exclude docs; only approved deployment files are copied.
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
	files, err := b.command("git", "ls-files", "-z", "--", "deploy", "scripts/scan.ps1", "src/identification/fingerprint/REFERENCES.md")
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
	readme := "# MusicFS 私有安装包\n\n此目录包含 Server 与统一内置插件程序（各角色仍独立启动进程），不包含音乐、账号凭据或开发数据库。\n\n" +
		"配置示例为 configs/server.example.yaml，内置插件自动发现，配置齐全且未显式禁用时自动启动。手动扫描使用 [PowerShell 扫描脚本](scripts/scan.ps1)。\n\n" +
		"安装包不包含 docs 目录；安装部署、配置、运行扫描及备份恢复说明请在对应 revision 的 Server 私有源仓库 docs 目录查阅。\n\n" +
		"release.json 记录版本、构建平台与依赖许可证；SHA256SUMS 校验文件一致性。Go 工具链许可证位于 licenses/go。\n\n" +
		"该制品仅供私有安装验证，不自动上传或公开分发。\n"
	return os.WriteFile(filepath.Join(b.out, "README.md"), []byte(readme), 0600)
}

func releaseDocument(name string) bool {
	return deploymentFiles[name] || name == "scripts/scan.ps1" || name == "src/identification/fingerprint/REFERENCES.md"
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
