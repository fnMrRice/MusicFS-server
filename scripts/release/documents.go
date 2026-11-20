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
	const readme = `# MusicFS Server 安装包

简体中文 · [English project guide](https://github.com/fnMrRice/MusicFS-server/blob/main/docs/README.en.md)

MusicFS Server 扫描音乐目录，管理曲库、歌词与封面，并为客户端提供音频。官方插件默认随服务端启动。包内程序与版本见[构建清单](release.json)，文件摘要见 [SHA256SUMS](SHA256SUMS)。

## 本机启动

如果安装目录有 start.bat 或 start.sh，直接使用对应启动脚本即可。单独运行 Server 时，首次先将[配置示例](configs/server.example.yaml)复制为 configs/server.yaml，然后在安装目录执行对应平台的命令。

**Windows PowerShell**

~~~powershell
$env:MUSICFS_ADMIN_TOKEN = (& ./bin/musicfs.exe token).Trim()
./bin/musicfs.exe serve --config ./configs/server.yaml
~~~

**Linux**

~~~sh
export MUSICFS_ADMIN_TOKEN="$(./bin/musicfs token)"
./bin/musicfs serve --config ./configs/server.yaml
~~~

上述第一行生成管理员令牌。已有令牌时，在环境变量 MUSICFS_ADMIN_TOKEN 中设置原值即可。启动前请将令牌保存在自己的密码管理器中，用于登录。

默认地址为 <http://127.0.0.1:8080>，可通过[就绪检查](http://127.0.0.1:8080/api/v1/health/ready)确认服务状态。带 Web 的安装包可以直接在该地址登录，进入「设置 → 音乐源 → 本地音乐」添加音乐目录。需要格式探测和转码时，请安装并配置 FFmpeg 和 ffprobe。

[配置指南](https://github.com/fnMrRice/MusicFS-server/blob/main/docs/operations/configuration.md) · [备份与恢复](https://github.com/fnMrRice/MusicFS-server/blob/main/docs/operations/backup.md) · [许可证](LICENSE)

本项目几乎完全由 AI 生成。
`
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
