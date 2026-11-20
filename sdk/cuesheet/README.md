# CUE 音轨解析

`cuesheet.Parse(text string) (Sheet, error)` 将 CUE 文本解析成包内相对文件引用和音轨时间区间。调用方负责在已授权目录中解析引用、探测文件和校验区间。解析器本身只处理文本。

## 音轨如何划分

- 每个 `FILE` 有独立时间轴，以每秒 75 帧的 `int64` 表示。音轨的 `FileIndex` 指向 `Files` 中的文件。
- 每轨须有一个 `INDEX 01`，可在此前包含一个 `INDEX 00`。同一文件的索引不能倒退，各轨的 `INDEX 01` 须严格递增。
- `StartFrames` 取本轨的 `INDEX 01`；右端点 `EndFrames` 取同文件下一轨的 `INDEX 01`，且不含该帧。下一轨的前间隙归入前一轨；首轨开始前的内容保留其索引，不生成隐藏音轨。
- 每个文件的末轨 `EndFrames` 为 `nil`，调用方据实际文件时长补全。作品和音轨的 `TITLE`、`PERFORMER` 分别保留，缺省值为空。

## 支持的语法

| 项目 | 规则 |
| --- | --- |
| 编码与分隔 | UTF-8，可带 BOM，兼容 LF/CRLF。关键字忽略大小写，带空格的值使用双引号 |
| 文件与音轨 | `FILE` 支持 `WAVE`、`AIFF`、`MP3`。音轨须为 `TRACK nn AUDIO`，轨号 1–99、全局递增，可跳号 |
| 资料与注释 | 解析 `TITLE`、`PERFORMER`。整行忽略 `REM`。对 `CATALOG`、`CDTEXTFILE`、`SONGWRITER`、`ISRC` 校验单值语法后忽略 |
| 未支持指令 | `PREGAP`、`POSTGAP`、`FLAGS`、其他索引号、非音频轨及未知指令会返回 `UNSUPPORTED_CUE` |

最多 99 个文件、99 条音轨，输入至多 1 MiB，单行至多 16 KiB。`mm:ss:ff` 的分钟至少两位，秒和帧各两位且分别小于 60、75。文件类型是 CUE 声明，实际编码须由宿主探测。

路径统一为 `/` 并折叠 `.` 与包内有效的 `..`。绝对路径、盘符、UNC、逃出包的引用、Windows 保留名、尾点或尾空格等歧义名称均被拒绝。宿主打开文件时仍须检查目录授权、符号链接及路径别名。

失败返回 `*cuesheet.Error`，`Code` 为 `INVALID_CUE`、`UNSUPPORTED_CUE` 或 `LIMIT_EXCEEDED`。行号从 1 起，文档级错误为 0。失败时不返回部分音轨。[解析实现](parse.go) · [语法测试](parse_test.go) · [路径测试](path_test.go)
