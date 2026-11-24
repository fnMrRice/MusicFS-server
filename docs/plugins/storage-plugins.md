# 持久化后端插件

提供受信任后端接口、内置 SQLite、内置 PostgreSQL 及独立 SQLite Worker。MySQL、远程对象存储和跨语言存储 SDK 尚未实现。外部存储属于拥有全部核心数据访问能力的基础设施插件，不使用普通 Provider 的角色或权限。

**数据库是必需的基础设施，不能被禁用。** `storage` 没有 `enabled` 开关；`enabled: false` 属于非法配置。管理插件列表展示已选择的数据库并标记 `required=true`、`stateMutable=false`，但不提供普通插件的启停操作。选择独立 SQLite 插件后，程序缺失、校验失败或退出都不能用停用/旁路继续提供正常存储服务，也不自动切换数据库。已有部署更换数据库引擎时，需停服并迁移数据，不能把“无数据库”作为运行模式。

内置 SQLite 使用 `musicfs.plugin.storage.sqlite` 身份；`storage.backend=plugin` 的协议目前只验证发行号而未声明具体后端身份，因此展示为 `musicfs.plugin.storage.plugin` /“外部数据库”，即使使用官方 SQLite Worker 也不从程序路径或参数猜测名称。`version` 使用实际成功握手的后端版本，绝不使用宿主传输适配器版本代替；其他受信任内置驱动使用其注册发行声明。`storage` 投影仅包含 `backend`、`mode=builtin/process` 和 `runtimeState=ready/unavailable/unknown`，可用性来自已打开后端的宿主内存信号，不在读取列表时探测数据库。基础设施没有普通管理实例，`instances=[]`；修改其状态返回 `UNSUPPORTED`，既不保存开关也不关闭数据库。

## 1. 配置与启动

发布配置默认在 Server 主进程直接使用 SQLite 插件，主库与实例状态库均不启动插件进程。存储是必需角色，部署 YAML 省略后端时使用此默认值：

```yaml
storage:
  backend: sqlite
```

### PostgreSQL

在 PostgreSQL 中准备独立数据库，将该库的 `public` Schema 的使用和建表权限授予 MusicFS 账号。Server 通过原生 Go 驱动 pgx 连接，首次启动自动执行 PostgreSQL 迁移。主库和插件私有状态共用此数据库，私有状态按插件、实例和键隔离。一个数据库同时供一个运行中的 Server 使用。

```yaml
storage:
  backend: postgres
  options:
    host: db.example.net
    port: '5432'
    database: musicfs
    user: musicfs
    passwordFile: ./secrets/postgres-password
    sslMode: verify-full
    sslRootCert: ./certs/database-ca.pem
```

`host`、`database` 和 `user` 必填。端口默认 `5432`，选项值均为字符串。密码可用 `passwordFile` 指向单行密码文件，或用 `passwordEnv` 指定提供密码的环境变量名。密码文件应存放在数据备份目录之外，例如 `/run/secrets/musicfs-db-password`。`passwordFile`、`sslRootCert` 的相对路径以配置文件所在目录为基准。`sslMode` 默认 `verify-full`，也支持 `verify-ca`、`require` 和 `disable`。使用私有 CA 时指定 `sslRootCert`。

新部署选择后端后即可启动。已有曲库切换引擎须停服并迁移主库及插件状态，编辑 YAML 不会复制现有数据。PostgreSQL 的离线备份使用同一事务导出主库和私有状态，恢复命令按便携格式生成 SQLite 部署。

开发验证时，将 `MUSICFS_TEST_POSTGRES_DSN` 设置为测试 PostgreSQL 的连接 URL，使用有 `CREATEDB` 权限的测试账号。在 Server 仓库运行 `go test -count=1 -run Postgres ./tests/integration/storage ./tests/integration/backup ./tests/e2e`。每个用例独立创建数据库并在结束时清理。

### 外部存储进程

需要显式外部存储模式时，再构建兼容入口并计算实际程序摘要：

```powershell
go build -o build/musicfs-plugins.exe ./src/app/musicfs-plugins
(Get-FileHash build/musicfs-plugins.exe -Algorithm SHA256).Hash.ToLowerInvariant()
```

将输出摘要填入部署 YAML。以下占位值必须替换：

```yaml
storage:
  backend: plugin
  execution: process
  executable: '../build/musicfs-plugins.exe'
  sha256: '<实际的64位SHA-256>'
  args: ['--plugin', 'storage-sqlite']
```

旧官方 SQLite Worker 配置未显式选择 `execution: process` 时在内存中映射到内置后端，不改写磁盘配置或数据库布局。路径相对于配置文件解析。选择、程序、摘要和参数来自启动配置，不从尚未启动的数据库读取。显式外部模式下，主库和 `host.database.*` 的实例私有状态读写进入持久化 Worker；主库保持一个受管进程，私有状态按作用域连接创建短期进程。两种模式的配置预加载都只读，不创建或迁移数据库；写入初始化仍在取得 Server 数据锁之后进行。

主库建库 SQL、内嵌资源和迁移加载器由 [`plugins/database/sqlite/migrations`](../../plugins/database/sqlite/migrations/README.md) 维护；实例配置及私有状态库的建表、迁移与只读读取由 `plugins/database/sqlite/state` 维护。插件在打开可写后端时自动完成初始化，成功后才返回 Backend 或发送 ready，部署时无需另行执行建库脚本。初始化失败拒绝该连接，不将部分就绪的数据库交给宿主。只读工厂不创建目录、数据库或表，也不执行迁移。

独立入口 `plugins/database/sqlite/cmd/musicfs-plugin-storage-sqlite` 保留，可显式单独构建并使用空 `args`；它不再是默认安装包中的单独文件。切换已固定的程序路径时需同步审核并更新摘要。

未知后端、摘要不符、协议不兼容或初始化失败均拒绝启动，不回退到空库。内置 SQLite 与官方 SQLite Worker 使用同一数据布局，可在正常停服后切换；其他数据库实现需要自行完成一致的数据迁移，不能只修改名称假装转换成功。

`storage.options` 为受信任后端的字符串选项；官方 SQLite 不接受额外选项。管理快照不包含后端连接选项、外部程序参数或路径。数据库凭据应由插件自己的受保护凭据机制提供，不提交到 Git；普通 YAML 及备份也不应用于存放明文数据库密码。

## 2. 后端契约

`src/storage.Backend` 汇总各领域的 Repository 与关闭接口，`Maintenance` 提供离线快照和恢复维护接口。一个 Backend 是一个一致性域，不为每个表分别选择实现。

| Server 定义的规则 | 后端执行责任 |
| --- | --- |
| 来源权限、稳定身份、修订、幂等、人工锁和完整发布语义 | 在对应业务事务中检查、更新和提交，不能拆成独立读写后再拼接 |
| 业务数据契约、启动锁和发布升级流程 | 维护具体 Schema 与迁移版本、内嵌建库资源，在返回可用后端前完成初始化；不支持的版本拒绝 |
| 文件引用与删除协调 | 保持文件保护回调覆盖提交或受保护删除的完整范围 |
| 备份清单和恢复验收 | 提供完整、可静态校验的便携 SQLite 快照及引用集合 |

原 SQLite 实现中的事务内权限和修订检查继续保留。后端实现者必须遵守这些契约；普通 Provider、Scraper、DSP 无权调用该接口或执行任意 SQL。

同程序内扩展可注册完整 `storage.Driver`。外部扩展使用 `storage/rpcbackend.Serve` 包装实现，宿主通过 `storage.backend=plugin` 选择可执行程序，不需要把新实现编译进宿主。首版外部 SDK 使用 Go：插件与宿主须匹配完整 Repository/DTO 合同；它不承诺任意旧版本二进制兼容或 C++ 通用 ABI。

`matching.Repository.BulkMerge` 要求存储后端在同一事务中校验来源权限、完整录音成员和排除关系，并持久化修订、幂等统计与审计。使用独立存储 Worker 时须与 Server 配套构建。

Driver 同时提供主库、维护、实例状态初始化 `OpenPluginState` 及实例状态只读工厂 `ReadPluginState`，私有状态的实现随所选数据库驱动提供；`src/plugins/plugindb` 只保留状态接口、选择和 RPC 调度。宿主继续定义实例命名空间、原子比较更新、事务和文件发布契约，并协调数据锁及生命周期。当前内置后端和静态备份校验仍使用 SQLite 插件代码；源码迁移不代表宿主已经消除全部 SQLite 依赖。

配置预加载、默认目录授权和 Worker 私有状态均调用所选驱动的状态工厂，并传入同一 `dataRoot` 与 `storage.options`。宿主不以 SQLite 文件是否存在决定是否读取；首次读取不存在状态的行为由驱动实现，SQLite 返回 revision=0。进程模式同样传递部署选项，只读预加载不创建状态目录或调用初始化工厂。SQLite 便携备份是明确的导出契约，与运行时状态后端分派相互独立。

`storage.Driver` 必须分别声明 `Version: storage.ContractVersion` 和 `PluginVersion`：前者是整数接口合同版本，后者是最多 128 字符的有效 SemVer 发行版本。注册后端及启动 `rpcbackend.Serve` 时都拒绝缺失、空白或无效发行版本。官方 `sqlite.Driver()` 使用 [sdk/bundledplugin/releases.go](../../sdk/bundledplugin/releases.go) 中的 `SQLiteVersion`，其构建值可在安装包的 `release.json` 中核对；内置后端和独立 Worker 共用这一声明。外部入口将完整 Driver 交给 `rpcbackend.Serve(ctx, os.Stdin, os.Stdout, driver)`，ready 的发行号由 `driver.PluginVersion` 提供，不能用合同整数代替。

宿主传输适配器自身的 `rpcbackend.Driver().PluginVersion` 当前为 `0.1.0`，仅标识适配器发行版本；实际外部后端版本来自成功握手后的 `Process.PluginVersion()`，两者不可混用。

## 3. 通信与故障

内置 SQLite 通过直接 Go 后端接口调用，和 Server 共享地址空间；取消依赖上下文与实现协作，不能安全强杀数据库线程。崩溃或内存破坏可能影响整个 Server。下面的 stdio、进程摘要和进程回收规则只适用于显式外部模式。

持久化协议标识为 `musicfs-persistence/1`，使用独立 stdio 连接、随机握手 nonce 和完整 Go 接口/导出字段形状摘要。`persistence.hello` 的 ready 响应必填 `pluginVersion`，宿主在接纳连接前校验其有效 SemVer；缺失或无效时拒绝握手并回收进程。该字段独立于协议标识和接口形状摘要，适用于 Backend、Maintenance 及实例状态模式。Server、统一插件程序及独立 SQLite 入口须配套升级并更新固定摘要；旧 Worker 即使协议和合同仍匹配，也会因未提供发行版本被拒绝。底层入口签名为 `pluginrpc.Serve(ctx, input, output, version, contracts, factory)`。

只有显式 Backend、Maintenance 或实例状态接口的方法可调用。修改接口后运行：

```text
go run ./scripts/generate-storage-rpc
```

生成的类型化客户端按领域操作调用，完整扫描发布等仍是一次原子操作。参数使用 Go gob 编码，保留公开 HTTP JSON 中隐藏的内部归属、代次和扫描计划字段；音频、歌词正文和音乐文件不会因此进入数据库。

数据采用有界消息。持久化连接上限 64 MiB、单值上限 48 MiB，超限失败；普通 Provider 控制连接仍保持 1 MiB。大批量操作在进入传输前受资源上限约束，不以拆分事务绕过限制。并发请求和回调均有上限。

GC 删除回调与 `Guard(func() error)` 使用不可猜测、只在原调用期间有效的回调标识。嵌套回调把受保护提交送回原 Worker 执行，文件保护与数据库事务不会在进程边界被拆开。取消后过期回调拒绝执行。

进程摘要在启动前验证，子进程环境不继承 Server 凭据；退出或取消时回收进程树。进程隔离不是恶意插件沙箱，安装者必须信任实现。断线、超时和进程崩溃不会自动重试写入：可能已经提交的请求应按原业务幂等键或读取操作结果确认，不能直接报告写入失败就认定没有副作用。

领域错误码、取消、截止时间和记录不存在保留；底层驱动异常不回显 SQL、路径或连接凭据。后端故障可能使整个服务不可用，不能按普通可选媒体效果旁路。

显式外部模式的必需存储进程退出或连接断开后，`/api/v1/health/ready` 返回 503；检查仅观察现有连接状态，不发送数据库请求、不自动重启或重试写入，也不覆盖原有启动和排空状态。

## 4. 备份和恢复

外部存储首版统一导出当前 MusicFS Schema 的 SQLite 便携快照，即使后续插件使用其他实时数据库，也必须实现这一导出契约。备份清单记录后端名称及存储/DSP 程序摘要；现有未记录后端的备份按 SQLite 读取。

验证备份只使用宿主的静态快照检查器，绝不执行备份配置中的程序。恢复到新目录后固定选择内置 SQLite，并清空 DSP 链及外部 DSP 配置，继续禁用来源/音频插件、自动任务和写回。原配置保存用于管理员复核；恢复不自动启动原存储或 DSP 程序。

原库、实例配置库、引用媒体和其他持久状态仍按[完整备份](../operations/backup.md)协同保存。备份不能只复制运行中的 SQLite 主文件而忽略 WAL；更换后端也不改变文件与数据库之间的发布和恢复要求。
