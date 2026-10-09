# YAML 配置与迁移

状态：新增要求，待实现；2026-10-09。当前 `src/config/config.go` 仍通过 JSON decoder 加载配置，实际示例仍为 `configs/server.example.json`。本文定义后续目标，不能直接把现有运行命令的扩展名换成 YAML 并称为可用。返回[实施入口](../implementation.md)。

## 1. 格式与唯一来源

Server 配置文件统一使用 YAML，正式示例为 `configs/server.example.yaml`，本机文件建议为 `configs/server.local.yaml`；接受 `.yaml` / `.yml`。插件注册、实例参数和启用开关也写在该 YAML 中。HTTP JSON、JSON-RPC、JSON Schema 与媒体清单保持各自协议格式，不随配置文件改变。

首版使用一份配置文件，不引入多层 include 或独立的启用状态配置。普通启动参数采用“默认值 → YAML → 明确支持的环境变量 → 命令行”次序；哪些字段可覆盖写入文档和测试。插件及实例的 `enabled` 只从 YAML 读取，不允许其他来源悄悄覆盖管理器保存的开关。敏感值继续由环境变量或凭据引用注入，不由管理器写回文件。

数据库可保存运行观察、审计、操作进度和实例业务状态，但不保存另一份权威启用开关。管理器和启动器通过同一配置服务读写。配置变更需要明确应用：管理操作自动应用本次开关，手工编辑通过配置重载或重启应用；首版不自动监视并应用任意文件变化。

## 2. 目标结构示例

以下只是目标字段草案，尚不能交给当前程序加载；实际 Schema、程序与测试一起落地。路径相对配置文件所在目录解析，Windows/Linux 示例在迁移时分别验证。`pluginId` 与 `instanceId` 是持久身份，不由数组下标或显示名称生成。

```yaml
configVersion: 1
listen: '127.0.0.1:8080'
allowLoopbackHttp: true
allowedOrigins:
  - 'http://127.0.0.1:8080'
dataRoot: '../data'
mediaRoot: '../media'
cacheRoot: '../cache'
plugins:
  - pluginId: 'musicfs.localfiles'
    executable: '../build/windows/musicfs-plugin-local.exe'
    enabled: true
    instances:
      - instanceId: 'local'
        role: 'provider'
        enabled: true
        config:
          musicRoots:
            - id: 'music'
              name: 'Music'
              path: '../sample-music'
  - pluginId: 'musicfs.audio.ffmpeg'
    executable: '../build/windows/musicfs-plugin-audio-ffmpeg.exe'
    enabled: true
    instances:
      - instanceId: 'audio-default'
        role: 'audio'
        enabled: true
        config:
          ffmpegPath: '../tools/ffmpeg.exe'
          ffprobePath: '../tools/ffprobe.exe'
```

摘要、允许版本、资源限制及其他现有服务字段继续支持，在正式示例和 Schema 中列出，示意文本不表示可以省去现有鉴权和路径校验。主配置里的插件可执行路径只由本机管理员配置，启停管理接口不能替换路径或执行任意程序。

插件与实例未写 `enabled` 时默认 false；仓库示例对选定的官方插件显式写 true。一个插件未配置、全局禁用或实例禁用，都不得因其他路径字段非空而自行启动。

## 3. S-P1-07：替换解析与配置模型

1. 在 `src/config` 按解析、模型、校验、修订和写入拆分源码；测试仍在根 `tests/unit/config` 和 `tests/integration/config`，不把 YAML 处理堆入入口。
2. 选择支持严格字段验证和语法树编辑的 Go YAML 依赖，固定已核实版本与许可证。不要自行用正则实现解析，也不通过 YAML 转 JSON 后忽略重复键/类型差异。
3. 只接受一个顶层 mapping 文档；限制输入大小与嵌套深度，拒绝重复键、未知字段、非法类型和额外文档。首版禁用自定义标签、merge key 和 anchor/alias，错误指出字段/位置而不泄露原始敏感值。
4. 明确字符串、布尔值、数值与 null 的处理：`enabled: false` 必须保留；字符串 `"false"` 不自动转为布尔值，空值与缺省分开。版本未知或重复插件/实例 ID 在任何进程启动前失败。
5. 先解析服务字段，再按角色配置 Schema 校验实例参数；插件未启用时仍校验结构与身份，但不要求其可执行程序或外部音频工具当前可运行。启用时才核验运行依赖，失败进入可诊断状态。
6. 保留相对路径基准、音乐根与可写目录隔离、TLS/Origin、资源上限和凭据脱敏的现有校验；格式迁移不能削弱这些行为。

验收：中文/空格/Windows 反斜线、Linux 路径、注释、false、null、未知键、重复键、多文档、错误类型、超长/过深内容均有固定样例；启动和重载失败不会产生部分生效的配置。

## 4. S-P1-08：可写配置与修订

1. 配置服务维护原始 YAML 的修订标识及当前应用修订。管理读取返回脱敏的有效配置、开关来源和是否可写；不用重序列化后的默认值覆盖用户原文件。
2. 管理变更只修改指定插件或实例的 `enabled` 节点，保留无关配置与注释。不把环境变量、命令行覆盖值、账号密钥写回 YAML。
3. 更新要求预期修订：用配置服务锁串行化本进程写入，写前复核文件版本并校验完整新文档。磁盘修订与已加载的期望配置修订不一致时，返回 CONFIG_NOT_APPLIED，要求先重载/重启；不能把磁盘新配置开关与运行旧路径/账号拼接执行。该已加载修订不要求全部实例已经排空。其他管理请求已改动则返回冲突；人工编辑应通过停服/协作锁或管理入口协调，不宣称普通文件替换可防住不遵守锁的外部编辑器。
4. 在同目录写临时文件、保留受限权限、完成持久化后执行目标平台验证过的原子替换。失败保留原有效文件并返回错误，不能只改内存就回应已持久化。文件只读、目录不可替换或存储不支持该能力时拒绝管理写入。
5. 持久化成功后按[插件管理](plugin-management.md)的代次与状态机应用开关；“已保存禁用意图”和“运行进程已退出”分别报告。应用失败不撤销已成功保存的用户意图，报告 failed/待处理原因。
6. 替换 YAML 前先持久记录操作意图，关联幂等键、旧/新配置修订和目标；替换后再记录保存结果并应用。重启时以已保存 YAML 为准重建状态，逐项复核未完成意图：仍为旧修订则未保存，为预期新修订则继续应用，已被更新修订替代则标 superseded；不能重放旧操作覆盖新配置。文件与 DB 不被假设为同一事务，所有中断点纳入集成测试。
7. 手工重载先完整解析/验证，失败保留当前运行配置；开关变化复用管理状态机。路径、账号和处理参数变化需要各自重建规则，首期可明确要求重启，不能当作开关变化热修改活动管线。

验收：并发修订冲突、只读文件、写入失败、替换前后崩溃、注释保留、脱敏值不写回、手工重载失败和重启持久化。只读容器挂载需要通过修改部署 YAML 并重启管理，接口不能暗中在 DB 覆盖文件。

## 5. S-P1-09：迁移现有 JSON 使用点

1. 用 `git mv configs/server.example.json configs/server.example.yaml` 移动已跟踪示例，再转换内容；不重建后删除。既有用户配置不自动覆盖，提供明确转换说明并保留原件。
2. 将 `pluginPath/audioWorkerPath` 等分散字段映射到显式插件/实例配置；保留已有 Local Files 实例 ID、根 ID、账号引用和持久状态目录，避免迁移后重复建库或丢失关联。
3. 同步配置模型标签、CLI 帮助、测试夹具、E2E 配置生成、忽略规则、构建包示例和 `docs/running.md` 等运行说明。不把协议测试里的 JSON 误改成 YAML。
4. 用原版本 JSON 配置的已知场景验证转换后的 YAML 行为等价；旧格式如不兼容，明确提示迁移方式，不静默当空配置。新启用字段由转换步骤显式填入，不因默认 false 导致已配置来源悄悄失效。
5. 在 Windows/Linux 分别从 YAML 启动、扫描、原流播放、处理后播放及重启；只有实际通过后，才把运行文档中的命令换为 YAML 并记录迁移完成。

完成条件：配置加载、管理写入和部署入口共用 YAML 模型；仓库真实运行路径不再依赖 JSON 配置；现有 JSON API/插件协议不受影响。
