# Web 本地 DSP 与服务端转换边界

Web 创建播放会话时提交 `preferences.dspExecution: client`，播放响应必须回显 `dspExecution: client` 才允许接入本地效果链。该值在会话创建时固定，定位、同源恢复及有有效对齐证据的跨源恢复沿用同一归属。

`client` 模式忽略请求的 `gainDb`，剥离个人、全局与部署效果链，不执行内置增益或外部 DSP；个人或全局的输出布局仍用于必要声道转换。原音频格式已兼容时可直接代理；解密、必要解码、声道布局转换、格式转换与转码仍在服务端完成。`forceProcessing` 仅强制转换路径，不恢复听感效果。`effectiveProcessing` 只描述服务端实际操作，允许 `ncmDecrypt` 与 `channelMix`；浏览器自己的参数不写入服务端描述。

处理器以不可变无音效视图执行每个会话，保留共享配额和插件生命周期门禁。缓存身份含无音效标记、零增益、输出布局及工具版本；与有音效会话隔离。恢复使用创建时的视图，之后修改个人或全局效果不会重新开启服务端处理；部署变化仍需按原规则重启播放。

本地 DSP 会话的人工时间轴证据通过 `GET /api/v1/playback/recovery-profile?dspExecution=client` 获取无效果处理身份；提交证据时服务端核验该身份和零服务端增益，不接受旧服务端效果链的证据自动继续。

`audioDSP.enabled` 默认 `false`，示例部署也显式关闭。禁用时不装配任何听感效果或外部 DSP，忽略历史个人、全局和部署效果链，保留 `audioChannels`。管理列表的音效条目显示 `configuredEnabled=false`、`chainState=disabled`。原实现与配置保留，日后明确设为 `true` 并重启即可恢复服务端效果；客户端仍可用 `dspExecution=client` 保持本地处理。

省略请求执行归属时按 `server`，但部署禁用仍优先。更新代码和配置后需部署新版并重启；不会修改已运行旧服务或现有数据。

音频 Worker 新增 `disableEffects` 请求字段，官方实现版本升为 `0.5.0`；宿主拒绝旧 Worker，避免旧实现忽略旁路指令。见 [HTTP 契约](../contracts/http.schema.json)、[音频插件协议](../contracts/plugins/v1/protocol.schema.json)。
