# P0：工程布局、工具链与契约

状态：工程与契约已落地；验证边界见[实施记录](../validation/progress.md)。返回[Server 实施入口](../implementation.md)。本章保留用户指定 `src/`、`src/app/`、根 `tests/` 结构的任务与门槛。

## 1. 目标目录

以下是随阶段逐步创建的目标，不要求 P0 生成所有空目录。

```text
server/
  go.mod
  go.sum
  README.md
  AGENTS.md
  sdk/                    # Independent Go module, owned by Server
    go.mod
    pluginproto/          # Versioned wire protocol and schemas
    online/               # Shared DTOs, no platform implementation
    provider/             # Reusable provider worker
  plugins/
    unified/              # Composition of nine roles for musicfs-plugins
    source/
      localfiles/         # Includes cmd/musicfs-plugin-local
      online/             # Independent Go module, depends only on SDK
        go.mod
        cmd/              # Standalone and unified online executable entries
        netease/
        qqmusic/
        applemusic/
        internal/
    decoder/
      ffmpeg/             # Includes cmd/musicfs-plugin-audio-ffmpeg
      ncm/                # Includes cmd/musicfs-plugin-ncm
    scraper/
      local/              # Includes cmd/musicfs-plugin-scraper-local
    database/
      sqlite/             # Includes cmd/musicfs-plugin-storage-sqlite
    dsp/
      gain/               # Includes cmd/musicfs-plugin-dsp-gain
  src/
    app/
      musicfs/main.go
      musicfs-plugins/main.go
    bootstrap/            # Composition and lifecycle
    config/
    musicdirs/            # Conventional music-directory discovery
    logging/
    domain/               # Stable domain IDs and small value types
    auth/
    api/                  # HTTP routing, DTOs, handlers and middleware
    storage/
      migrations/         # Versioned SQL files and embed.go
    jobs/
    pluginhost/
    sourceaccess/         # Authorized source handles and input broker
    library/
    playback/
    stream/
    audio/
      dsp/
    identification/
    mediastore/
    enrichment/
    writeback/
    playlists/
    recommendations/
  tests/
    unit/
      config/config_test.go
      library/library_test.go
      playback/session_test.go
    contract/
      api/
      plugins/
    integration/
      storage/
      scan/
      plugins/
      playback/
      recovery/
    e2e/
    testkit/              # Importable helpers, never production imports
    testapps/             # Fake/crashing plugin programs
    testdata/
      audio/
      manifests/
      platform-responses/
  docs/
    contracts/            # OpenAPI and JSON Schema, created in P0
    implementation/
    validation/           # Recorded validation, created when executed
  configs/
    server.example.yaml   # Current versioned YAML configuration
  scripts/
  build/                  # Ignored build output
```

根 go.mod 的 module 路径为 `github.com/fnMrRice/MusicFS-server`，`src` 是宿主业务包目录。插件实现按用途归入 `plugins/source`、`plugins/decoder`、`plugins/scraper`、`plugins/database`、`plugins/dsp`。分类目录不等同于 Go 模块：SDK 使用独立模块 `github.com/fnMrRice/MusicFS-server/sdk`，在线平台实现使用 `github.com/fnMrRice/MusicFS-server/plugins/source/online`，其余当前实现属于根模块；三者由同一 Server 仓库维护。SDK 不导入宿主业务，在线模块只依赖 SDK，不导入 Server 的 `src`。开发时通过各 go.mod 的明确本地 replace 关联，构建和检查均在 `GOWORK=off` 下执行；协议版本与实现版本分别维护。

`src/app` 保留 Server 与全量统一插件程序的薄入口；`plugins/unified` 负责九种角色的装配，各实现仍按用途独立维护；单个插件入口位于各自 `plugins/<用途>/<实现>/cmd/<程序>`。可测试的启动、配置和生命周期逻辑放 `src/bootstrap` 等普通包；其他包和 tests 不导入 package main。Server、本地角色、SQLite 与 DSP 入口属于根模块；网易云、QQ 音乐、Apple Music 入口分别位于 `plugins/source/online/cmd/musicfs-plugin-<平台>`，可在该模块内单独构建。

不要用 `src/internal` 隐藏要由根 tests 导入的模块：Go 的 internal 导入限制会阻止 src 外的测试引用。当前 src 业务包仍是宿主实现接口，不承诺插件 SDK 兼容。插件使用版本化 wire 契约及 `sdk` 的 DTO、协议和 Worker 基础能力；SDK 的独立测试及示例验证其脱离宿主编译的边界。

当前示例、加载器及配置读写服务统一采用[YAML配置](../configuration.md)，旧JSON配置不兼容；[配置验收](../validation/configuration.md)记录本轮结果。HTTP、JSON-RPC与Schema继续使用各自协议格式。

## 2. P0 逐步任务

### S-P0-01：固定工具链和依赖基线

1. 选择实施时仍受支持的 Go 稳定版本，记录精确工具链；go.mod 的 go/toolchain 与 CI 保持可解释的一致关系，不用无限浮动的 latest。
2. 初始化根 go.mod；只添加当前阶段使用的依赖。SQLite 初始用 `database/sql` 加 `modernc.org/sqlite`，固定通过验证的版本。
3. 用相同 Go 版本分别构建 Windows amd64 与 Linux amd64。ARM64 先列待验证，不能由交叉编译成功推定运行支持。
4. 将 build、运行数据、缓存、凭据、本机配置和覆盖率产物加入忽略规则。示例配置不含用户磁盘路径和真实 token。
5. 建立依赖清单：用途、版本、许可证、原生工具需求；FFmpeg 构建独立记录，不能假定纯 Go 主程序意味着整个交付只有一个无依赖文件。

交付：go.mod/go.sum、工具链说明、空入口可构建证据。测试：全新机器或干净 CI 可下载固定依赖并构建，仓库不出现运行数据和凭据。

### S-P0-02：建立最小包依赖方向

1. `domain` 仅放被多个模块共同使用的小类型：UserID、SourceTrackID、AssetID、JobID、Revision；不放 SQL、HTTP、子进程或万能业务管理器。
2. `api` 把请求映射到应用服务；library/playback/enrichment 等服务通过自己需要的小接口访问存储和来源，不反向依赖 api。
3. `plugins/database/sqlite` 实现持久接口，`pluginhost` 实现 worker 控制，`sourceaccess` 实现授权读取；bootstrap 负责把实现注入服务。
4. 插件 wire DTO 与持久模型、浏览器 DTO 分离，在边界显式转换；平台字段不能沿 Provider 响应直接污染核心表。
5. 不创建无人调用的接口和空实现；先由 P1/P2 的调用需求决定接口形状。

交付：一张包依赖说明和可编译的最小组装。验收：无导入环，测试能用替身构建服务，生产包不引用 tests。

### S-P0-03：测试目录与测试策略

1. 单元测试在 `tests/unit/<模块>/*_test.go`，例如 package library_test 导入 `<module>/src/library`。同名 package 声明不会让不同目录共享未导出符号。
2. 通过导出的真实业务接口验证行为，替身实现被消费的小接口；不为了查看私有变量添加生产后门。复杂私有逻辑可先重构为有业务意义的模块。
3. 契约测试在 tests/contract；数据库、插件进程和文件系统组合测试在 tests/integration；跨制品流程在 tests/e2e。
4. 共享 helper 放 tests/testkit；需要崩溃、乱序、超时的伪插件入口放 tests/testapps。测试代码、helper、夹具和脚本不放 src。
5. 小型专用夹具可放测试目录自己的 testdata，共享音频放 tests/testdata。临时工作目录用测试提供的临时目录并可靠清理，不使用私人音乐库。
6. 运行真实浏览器的 E2E 在 Web 仓库维护；Server 的 e2e 校验 HTTP/进程/磁盘行为，双方使用同一夹具说明。
7. `scripts/check.ps1` / `scripts/check.sh` 在运行检查前拒绝 `src/**/*_test.go`。上述布局约束针对宿主源码；SDK 与插件独立模块在各自模块边界内维护测试。

启动组装测试通过 `bootstrap.Serve` / `ServeWithOptions` 验证 HTTP、权限与生命周期；插件宿主和 DSP 测试通过独立测试进程验证协议、异常与退出，RPC 测试通过真实通道验证序列化及回调生命周期，不读取私有状态。`musicdirs` 负责系统目录探测，启动组装将探测函数传给配置初始化，测试使用临时目录提供相同依赖，不扫描私人音乐库。

### S-P0-04：构建与检查入口

以下为检查入口；Windows/Linux 的实际执行范围与限制见[实施记录](../validation/progress.md)，不能由一处通过推定所有目标环境通过：

```text
go build -o build/musicfs ./src/app/musicfs
go build -o build/musicfs-plugin-local ./plugins/source/localfiles/cmd/musicfs-plugin-local
go test ./...
go test -coverpkg=./src/...,./plugins/... -coverprofile=build/coverage.out ./tests/...
go vet ./...
go test -race ./tests/...
```

Windows 制品使用 `.exe` 后缀。日常构建使用 `scripts/build.ps1` / `scripts/build.sh`，默认只输出 `musicfs` 和 `musicfs-plugins`；显式 `-SplitPlugins` / `--split-plugins` 时额外输出六个分拆程序；检查使用 `scripts/check.ps1` / `scripts/check.sh`。检查脚本覆盖 `src`、`tests`、`scripts/release`、`sdk` 与全部 `plugins`，并分别运行各 Go 模块的 vet 与测试。`-race` 在支持平台和具备所需工具链的 CI 运行；普通交叉构建、race 构建和真实目标机运行分别记录。

外置 tests 的覆盖率显式指定 `-coverpkg=./src/...,./plugins/...`，包含宿主与根模块插件实现。根目录的 `go test ./...` 只发现根 module 中的测试，不跨越 `sdk/go.mod` 与 `plugins/source/online/go.mod`，必须另行进入两目录测试；根 `tests` 下不另建 go.mod。集成测试明确通过 build tag 或测试开关启用，CI 和文档列出对应命令，不能把跳过外部依赖的成功输出当完整通过。

### S-P0-05：最小 HTTP 与插件契约

1. 在 docs/contracts/openapi.yaml 定义[共同实施契约](../../../docs/implementation.md)中的列表、鉴权、扫描、播放和错误 envelope。
2. 定义毫秒/字节/bit/s 单位、null 语义、ID、枚举、列表游标、状态错误、幂等键和尝试代次。
3. 在 docs/contracts/plugins/v1/protocol.schema.json 定义握手、manifest、控制请求/响应、分页、取消和错误；不混入 PCM 正文。
4. 通过有效和无效样本验证 schema。生成代码是否使用工具在 P0 决定；即使手写 DTO，也有契约一致性测试，避免双份模型漂移。
5. 约定废弃字段、兼容版本和 capability 演进流程；未实现能力返回 UNSUPPORTED，不返回看似成功的空内容。

### S-P0-06：媒体夹具与播放验收基线

1. 准备合法短音频：浏览器已验证直放格式、需处理格式、损坏文件、空文件、VBR、缺标签、中文/空格文件名。
2. 生成不同时间段和声道有可识别标记的音频，用于判断 seek 落点；记录时长、摘要、音量、生成命令和许可。
3. 记录预期 browser/OS/profile。codec 支持来自实际试验，不能从扩展名或 canPlayType 单一结果推定成功。
4. 建立 P3/P4 验收记录模板，包含声音、首帧、非零定位、取消、输入不变和资源回收证据。

## 3. 拆分和测试的共同要求

源文件目标不超过 400 行；入口保持薄，模块按解析、校验、服务、持久化适配、进程控制等真实职责拆分，测试按场景拆文件。因格式化或确实无法避免时允许适当放宽并说明原因，不增加无意义的转发层凑行数。生成代码的维护方式单独记录，不手工破坏生成结果；不要借生成文件豁免手写巨型模块。

## 4. P0 完成门槛

- [x] 用户指定目录、module 导入和外置 tests 在 Windows/Linux 构建验证。
- [x] 覆盖率包含 src；测试通过正式接口或明确的可信测试依赖注入验证。
- [x] HTTP/插件契约具备有效、无效及协议版本拒绝样本；不为未上线开发基线增加旧格式兼容。
- [x] 合成媒体夹具与 P3/P4 验收记录可复用，来源/许可明确。
- [x] 工程建立与各功能阶段分别记录，不用空包或构建成功证明可播放。

逐步证据见[核对表](../validation/documentation-audit.md#p0工程与契约)、[夹具](../validation/fixtures.md)和[制品验收](../validation/release.md)。上方目录树保留初始职责规划并反映 SDK/在线模块边界；当前默认构建 Server 与九角色统一插件程序，共两个程序；可选分拆构建额外提供三个在线平台、在线统一入口、SQLite 和 DSP gain，合计八个程序。以各模块入口与构建脚本为准；正式安装包的存储默认及可选 DSP 见[部署说明](../deployment.md)，实现包不要求与规划一一同名。

依据：[Go 模块组织](https://go.dev/doc/modules/layout)、[go.mod](https://go.dev/doc/modules/gomod-ref)、[Go 测试](https://pkg.go.dev/testing)。官方包/测试规则与本项目的目录选择分别说明，不将其他社区目录模板称为强制标准。
