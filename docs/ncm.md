# NCM 解密插件

本插件用于读取本地网易云 `.ncm` 文件，提取内部原有 MP3/FLAC 字节，再接现有播放与处理流程。没有实现网易云账号登录、在线歌曲下载或在线 Provider。测试使用项目自行生成的 CC0 合成音频封装，不代表所有真实客户端下载版本均已验证。

## 1. 边界与参考

- 独立程序：`musicfs-plugin-ncm`；插件标识 `musicfs.ncm`，角色 `audio`，能力 `audio.decrypt`。
- 核心采用 Go 标准库重写；参考 [taurusxin/ncmdump-go 的 MIT 实现](https://git.taurusxin.com/taurusxin/ncmdump-go/src/branch/main/ncmcrypt/ncmcrypt.go)及 [ncmdump.rs 的 MIT 实现](https://github.com/iqiziqi/ncmdump.rs/blob/master/crates/ncmdump/src/ncmdump.rs)。未引入这两个项目的运行依赖，也未移植 GPL 的 keuin/ncm 源码。
- NCM 是外层加密容器。解密结果保持内部编码字节，不将 MP3/FLAC 解码再压缩；后续仅在终端不兼容或用户要求 DSP 时进入已有 FFmpeg 管线。
- 原始文件只读，封面 URL 不下载，元数据不写回。封面数据跳过；曲名/艺术家可取自容器，时长/码率以内部音频的实际 ffprobe 结果为准。

## 2. 运行

运行 `scripts/build.ps1` 会额外构建 NCM Worker。现有配置增加：

```json
{
  "ncmWorkerPath": "../build/windows/musicfs-plugin-ncm.exe"
}
```

相对路径以配置文件目录为基准，Linux 使用对应无 `.exe` 程序。可选 `ncmWorkerSha256` 固定管理员审核的 Worker 摘要；省略时采用与其他官方 Worker 相同的显式路径信任与本次启动摘要检查。留空 `ncmWorkerPath` 即不启用 NCM 解密，其余格式仍可使用。

配置后重启 Server，对音乐目录重新扫描。列表容器显示 `ncm`，codec 显示内部音频格式。点击播放无需额外选项：内部 MP3 可交付固定解密表示并提供 Range；内部 FLAC 按当前播放策略转为 MP3。勾选服务端处理时解密后照常应用 Go DSP。

需要 FLAC 转码和准确的媒体参数时配置 `ffmpegPath`、`ffprobePath` 及音频 Worker。仅有 NCM Worker 时可读取 MP3，未探测到的时长/码率保持未知。损坏或不支持的 NCM 返回明确错误，不作为一轮扫描成功空库。

## 3. 数据与接口

Local Files 扫描纳入 `.ncm`，根据 `CTENFDAM` 文件头识别容器。解密插件通过版本化 JSON-RPC 握手与建立任务，文件字节通过随机、任务限定的 loopback HTTP Range 通道传递，控制面不装载音频正文。

插件从输入句柄读取，不收到原始文件路径；解析过程限制长度、解密块与元数据大小，读取内容可随机访问。一次任务固定输入、资产和 nonce；关闭或取消后回收进程及能力 URL。官方插件仍是受信任程序，独立进程不等于完整操作系统沙箱。

播放创建时将解密内容写入已有私有磁盘快照，受缓存预算限制；快照发布前重新校验原始文件版本。SourceAsset 仍指原始 NCM，解密表示使用自己的长度、ETag、representationId 和 attempt URL。其 Range 不引用加密原文件的字节偏移。

HTTP 播放描述的 `effectiveProcessing` 增加 `type: "ncmDecrypt"`、`execution: "server"`、`version: "1"`，处理链需要增益时再追加 `gain`。既有 Web 的扩展字段兼容此描述；纯解密交付模式为 `proxy`，解密后转码为 `processed`。插件 wire 字段见[协议说明](contracts/plugins/v1/README.md)。

## 4. 验证状态

本次正在验证核心、真实 Worker、Server 端到端与浏览器链路。完成后在此记录命令、结果和限制；不以自生成文件单独证明所有真实 NCM 文件兼容。
