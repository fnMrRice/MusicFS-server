# NCM 格式参考

本目录的 `core*.go` 是独立的 Go 实现，按格式重新编写有界头部解析和按偏移解密，不引入外部解码库。参考以下 MIT 开源实现所体现的格式布局与算法：

- [taurusxin/ncmdump-go 的 ncmcrypt.go](https://git.taurusxin.com/taurusxin/ncmdump-go/src/branch/main/ncmcrypt/ncmcrypt.go)；[MIT LICENSE](https://git.taurusxin.com/taurusxin/ncmdump-go/src/branch/main/LICENSE)。
- [iqiziqi/ncmdump.rs 的 ncmdump.rs](https://github.com/iqiziqi/ncmdump.rs/blob/master/crates/ncmdump/src/ncmdump.rs)；[Cargo.toml 中的 MIT 许可声明](https://github.com/iqiziqi/ncmdump.rs/blob/master/Cargo.toml)。

未复制或依赖 GPL 库。格式内的固定 AES 常量是文件格式算法参数，不是用户凭据；不会写入错误或日志。

当前只识别解密后的 MP3 或 FLAC，元数据的 `format` 字段不能改变识别结果。缺失元数据允许通过；加密区块、填充和容器长度错误会被拒绝。封面核对容量与长度后，以有效 `coverLength` 提供独立有界读取器；音频起点仍跳过完整 `coverCapacity`，不把填充算作图片。NCM 核心不解码图像，由宿主交给本地 Scraper 验证；不会将外层封面写入解密音频。CRC/保留区不做校验，NCM 本身不提供音频完整性的密码学保证；实际音频可解码性由后续探测与处理阶段确认。

测试使用独立合成容器，不代表已经验证真实客户端下载的 NCM 文件。核心只持有至多 4 KiB 密钥区块、1 MiB 元数据区块及派生流；封面容量上限 64 MiB，但不会把封面或完整音频读入内存。底层输入必须遵守 `io.ReaderAt` 契约且在读取期间保持不变。
