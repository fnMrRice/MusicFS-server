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
	for _, name := range []string{"LICENSE", "configs/server.schema.json"} {
		if err := copyRegular(filepath.Join(b.root, name), filepath.Join(b.out, name), 4<<20); err != nil {
			return err
		}
	}
	for _, mod := range sourceModules {
		for _, name := range []string{"go.mod", "go.sum"} {
			path := filepath.Join(mod.Directory, name)
			if _, err := os.Stat(filepath.Join(b.root, path)); name == "go.sum" && os.IsNotExist(err) {
				continue // A module with no external dependencies needs no checksum file.
			}
			if err := copyRegular(filepath.Join(b.root, path), filepath.Join(b.out, path), 4<<20); err != nil {
				return err
			}
		}
	}
	files, err := b.command("git", "ls-files", "-z", "--", "deploy", "scripts/scan.ps1", "src/metadata/identification/fingerprint/REFERENCES.md")
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
	if err = b.containerConfiguration(); err != nil {
		return err
	}
	programNote := "默认包含 musicfs 和 musicfs-plugins 两个程序。"
	if b.splitPlugins {
		programNote = "本包显式启用分拆构建，除 musicfs 和 musicfs-plugins 外，附带 SQLite、DSP gain、三个在线平台和在线统一入口，共八个程序。"
	}
	readme := "# MusicFS 私有安装包\n\n" + programNote + "官方插件默认编译进 musicfs，在 Server 主进程内运行。musicfs-plugins 保留九种角色的可选外部进程入口，默认配置不会启动它。不包含音乐、账号凭据或开发数据库。\n\n" +
		"配置示例为 configs/server.example.yaml，内置插件自动发现，配置齐全且未显式禁用时自动启动。手动扫描使用 [PowerShell 扫描脚本](scripts/scan.ps1)。\n\n" +
		"在线平台在主进程中按用户和实例分别维护账号状态。普通插件只有显式配置 execution: process 才启动经过摘要校验的外部程序。\n\n" +
		"SQLite 是主进程内必需基础设施，不进入普通插件启停列表，不提供禁用开关；后端加载失败时拒绝启动。服务端听感效果默认禁用；显式开启 audioDSP.enabled 并配置链后才执行，必要声道转换独立保留。指纹分析、FLAC 标签处理、匹配评分和缩略图变换也通过内置插件接口调用；FFmpeg、ffprobe、fpcalc 外部工具仍需相应安装和验证。\n\n" +
		"容器只能使用 Linux 安装包；容器配置同样默认使用内置 SQLite 与插件。Windows 制品不能直接用作 Linux 容器安装。\n\n" +
		"安装包不包含 docs 目录；安装部署、配置、运行扫描及备份恢复说明请在对应 revision 的 Server 私有源仓库 docs 目录查阅。\n\n" +
		"release.json 记录版本、构建平台、Server/SDK/在线插件各模块及依赖许可证；各模块的 go.mod/go.sum 随包保留。SHA256SUMS 校验文件一致性。Go 工具链许可证位于 licenses/go。\n\n" +
		"该制品仅供私有安装验证，不自动上传或公开分发。\n"
	return os.WriteFile(filepath.Join(b.out, "README.md"), []byte(readme), 0600)
}

func releaseDocument(name string) bool {
	return deploymentFiles[name] || name == "scripts/scan.ps1" || name == "src/metadata/identification/fingerprint/REFERENCES.md"
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
