# Web 本地 DSP 与服务端转换边界

Web 创建播放会话时提交 `preferences.dspExecution: client`，播放响应必须回显 `dspExecution: client` 才允许接入本地效果链。该值在会话创建时固定，定位、同源恢复及有有效对齐证据的跨源恢复沿用同一归属。

`client` 模式忽略请求的 `gainDb`，剥离个人、全局与部署效果链及声道插件，不执行内置增益或外部 DSP；历史 `outputLayout` 不会强制扩声或转码。原音频格式已兼容时直接代理；解密、必要解码、格式转换与转码仍在服务端完成。转码按来源优先保留单声道、立体声、5.1 或 7.1，单声道使用 `mp3-mono-<码率>`，环绕声使用 Opus/WebM；终端或编码器不支持对应格式时，才由 FFmpeg 做编码兼容降混，不调用声道扩展插件。`forceProcessing` 仅强制转换路径，不恢复听感效果或扩声。`effectiveProcessing` 只描述实际插件操作，客户端模式可出现 `ncmDecrypt`，不会出现 `channelMix`；浏览器自己的参数不写入服务端描述。

处理器以不可变无音效、无声道插件视图执行每个会话，保留共享配额和插件生命周期门禁。缓存身份含两个禁用标记、零增益、实际输出档案及工具版本；与启用插件的会话隔离。恢复使用创建时的视图，之后修改个人或全局效果、布局不会重新开启服务端处理；跨源恢复若来源声道少于固定输出则要求重新开始，避免暗中扩声。部署变化仍需按原规则重启播放。

本地 DSP 会话的人工时间轴证据通过 `GET /api/v1/playback/recovery-profile?dspExecution=client` 获取无效果处理身份；提交证据时服务端核验该身份和零服务端增益，不接受旧服务端效果链的证据自动继续。

`audioDSP.enabled` 与 `audioChannels.enabled` 均默认 `false`，示例部署也显式关闭。禁用时不装配听感效果、外部 DSP 或声道插件，忽略相应历史设置。管理列表的音效条目显示 `configuredEnabled=false`、`chainState=disabled`，声道插件显示 `configuredEnabled=false`、`builtin.configured=false`。源码与历史配置保留，日后明确启用并重启可恢复服务端插件；客户端仍通过 `dspExecution=client` 完整旁路。

省略请求执行归属时按 `server`，但部署禁用仍优先。更新代码和配置后需部署新版并重启；不会修改已运行旧服务或现有数据。

音频 Worker 使用 `disableEffects` 与 `disableChannelMix` 请求字段，其官方实现版本见安装包 `release.json`；宿主拒绝旧 Worker，避免旧实现忽略旁路指令。HTTP 请求无需新增字段。见 [HTTP 契约](../contracts/http.schema.json)、[音频插件协议](../contracts/plugins/v1/protocol.schema.json)。
