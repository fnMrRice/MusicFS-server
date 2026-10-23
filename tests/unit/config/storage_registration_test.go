package config_test

// 配置加载通过驱动注册表预读状态，测试与程序入口显式装配同一默认后端。
import (
	_ "github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	_ "github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
)
