package library_test

// 测试装配显式注册官方格式；曲库实现只依赖公共识别接口。
import (
	_ "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	_ "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
)
