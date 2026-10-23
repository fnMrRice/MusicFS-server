package main

type releaseProgram struct {
	name, moduleDir, packagePath string
}

type sourceModule struct {
	Path      string `json:"path"`
	Directory string `json:"directory"`
}

var sourceModules = []sourceModule{
	{Path: "github.com/fnMrRice/MusicFS-server", Directory: "."},
	{Path: "github.com/fnMrRice/MusicFS-server/sdk", Directory: "sdk"},
	{Path: "github.com/fnMrRice/MusicFS-server/plugins/source/online", Directory: "plugins/source/online"},
}

var releasePrograms = []releaseProgram{
	{"musicfs", ".", "./src/app/musicfs"},
	{"musicfs-plugins", ".", "./src/app/musicfs-plugins"},
	{"musicfs-plugin-storage-sqlite", ".", "./plugins/database/sqlite/cmd/musicfs-plugin-storage-sqlite"},
	{"musicfs-plugin-dsp-gain", ".", "./plugins/dsp/gain/cmd/musicfs-plugin-dsp-gain"},
	{"musicfs-plugin-netease", "plugins/source/online", "./cmd/musicfs-plugin-netease"},
	{"musicfs-plugin-qqmusic", "plugins/source/online", "./cmd/musicfs-plugin-qqmusic"},
	{"musicfs-plugin-applemusic", "plugins/source/online", "./cmd/musicfs-plugin-applemusic"},
	{"musicfs-plugin-bilibili", "plugins/source/online", "./cmd/musicfs-plugin-bilibili"},
	{"musicfs-plugin-online", "plugins/source/online", "./cmd/musicfs-plugin-online"},
}

func (b builder) programs() []releaseProgram {
	if b.splitPlugins {
		return releasePrograms
	}
	return releasePrograms[:2]
}

func (b builder) programNames() []string {
	result := make([]string, 0, len(b.programs()))
	for _, program := range b.programs() {
		result = append(result, program.name)
	}
	return result
}
