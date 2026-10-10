# S-P7-02 在线匿名资料 HTTP 组合验收

日期：2026-10-10。本记录针对配置、公开宿主服务、HTTP与独立Provider组合。核心适配器、固定上游、SDK、独立进程及最初匿名外网证据另见[online-core.md](online-core.md)；不把合成Transport测试当作远端平台验收。

## 实现范围

三个固定插件ID `musicfs.netease/musicfs.qqmusic/musicfs.applemusic` 使用 `role: provider`、空 `config`，正式示例默认双层关闭。加载器及配置Schema拒绝URL、凭据、账号、musicRoots和导入默认项；宿主限制三平台合计最多16实例。启动校验实际程序及SHA-256/握手，禁用或启动失败保留offline声明，不替换实例。构建脚本增加三个独立Go程序，没有新依赖。

`onlinehost.Service` 按owner/实例匹配，账号固定匿名，内部配置修订绑定插件、实例、实际程序摘要和声明版本。公开列表不返回配置修订、路径或凭据。总查询并发4、期限10秒，超额429，取消传至Provider。API提供搜索、资料、匿名公共推荐、歌单观测和明确不支持的音频解析；读取不产生音频资产或隐式导入曲库。

## 合成与真实进程测试

`tests/unit/config/online_test.go` 同时检查加载器与Schema：三平台角色/双层启用、空配置、无程序、URL/token/account/roots/importDefaults拒绝。额外验证跨平台16实例总预算。正式YAML示例通过实际加载、文档解析与Schema的共同校验。

`tests/unit/onlinehost/service_test.go` 验证4个活动请求占满准入后第5个返回429及秒单位重试提示、调用取消到达所有Provider、owner404、非匿名账号UNSUPPORTED、typed nil Provider投影offline而不崩溃。

`tests/contract/api/online_http_test.go` 使用生产在线客户端及合成上游响应，验证：

- 只列本人实例，QQ/Apple不宣告未验证能力，offline与能力声明分开；匿名/私人实例不可读取。
- 实际搜索/资料/推荐/歌单DTO通过HTTP Schema，资料页不是音频地址，未知播放能力不被夸大；重复远程歌曲位置保留。
- 搜索游标不能跨实例或改查询，未知/重复query、非法limit、账号输入拒绝；上游QQ 500映射503，Apple缺授权401，限流429保留Retry-After。
- resolve `{}` 明确415且不要求幂等键，仍校验Origin/CSRF；外来字段拒绝。所有查询结束后本地来源表为空，没有隐式导入。

`TestOnlineActualServerAnonymousWorkerAndDisabledRestart` 读取真实YAML，启动正式Server组合、独立合成网易/QQ Worker以及正式Apple Worker。合成Worker复用正式在线实现，仅在测试入口注入固定响应；发布脚本不构建测试Worker。覆盖搜索、资料、公共推荐、完整unversioned观测/重复条目、限流、QQ上游失败、Apple授权缺失、三平台音频解析拒绝，以及停服后双层禁用重启仍offline。普通验收通过9.219秒（主体8.54秒）。该项不访问外网。

首次组合编译遇到其他并行步骤尚未落齐的类型/函数，等待那些代码可编译后才重新执行；此中间状态未算作验收通过，也不是上游平台故障。

## 本轮公开网络验证

显式设置 `MUSICFS_ONLINE_LIVE=1` 后，仅运行 `TestOnlineLiveAnonymousHTTP` 一次；默认测试跳过此项。使用正式Server、三个正式独立Provider程序和默认受限网络Transport，未注入平台响应。没有平台Cookie、Authorization、私人内容、指纹上传、账号登录或音频下载。搜索查询为MusicFS，搜索和推荐各请求1项；请求间保留限流间隔，验证记录仅保存数量和状态，不把真实曲目资料写入Git。

本轮结果在 **2026-10-10 05:13 UTC** 汇总：

| 操作 | 实际结果 |
| --- | --- |
| 网易公开搜索 | HTTP200，1条结果 |
| 网易该曲目资料 | HTTP200，返回身份与搜索一致，播放能力保持unknown |
| 网易公共推荐 | HTTP200，1项，非个性化 |
| 网易该推荐歌单 | HTTP200，40个条目，complete=true、consistency=unversioned |
| QQ匿名搜索 | HTTP503，经正式适配器映射为上游失败；未因此宣告supported |
| Apple目录搜索 | HTTP401 AUTH_REQUIRED，未接入开发者凭据 |
| 三平台音频resolve | 均HTTP415，不返回资源或播放会话 |
| 本地曲库 | 仍为空，没有隐式导入 |

公网组合测试通过14.595秒（主体13.95秒）。40条是这次推荐歌单的独立观测，不能与此前核心记录中另一歌单的209条比较为远端变更，也不能把complete解释为稳定远程版本。

## 最终定向并发回归

Windows/amd64，Go 1.26.9、UCRT64工具链，仓库固定依赖 yaml3.0.5 / x/sys0.49.0 / x/text0.43.0 / SQLite1.60.1 / JSON Schema6.0.3。没有变更go.mod或go.sum。

`go test -mod=readonly -race ./tests/unit/config ./tests/unit/onlinehost ./tests/contract/api ./tests/e2e -run 'TestOnline|TestOfficialYAMLExample|TestAllModelsCompile|TestContractSamples|TestOpenAPIReferencesAndOperations' -count=1`：

| 范围 | 结果 | 包耗时 |
| --- | --- | --- |
| 在线配置和正式示例 | 通过 | 3.132秒 |
| 宿主owner/并发/取消 | 通过 | 1.797秒 |
| 在线HTTP、样本、全部Schema/OpenAPI结构 | 通过 | 34.243秒 |
| 合成Worker真实Server/重启 | 通过 | 14.348秒 |

最终race未启用公网开关，没有重复平台探测。原生子进程和文件操作使用获准的沙箱外测试权限；所有本地状态位于测试临时目录。实际远端HTTP只在Windows验证，不新增浏览器、ARM/macOS/NAS或平台账号验收声明。

主步骤将P702精确文件冻结后，在Arch WSL2补验：全源/测试vet通过；online核心2.170秒、onlinehost1.037秒、provideronline1.200秒、配置2.073秒、插件契约4.416秒、HTTP全部契约55.422秒均通过race；实际Worker61.360秒、合成响应的真实Server组合25.939秒通过。Linux没有重复公网调用，也没有将fixture成功写成平台账号或音频实测。

当前能力没有推荐聚合缓存、个人歌单导入/同步、账号登录、音频版权/地域权益验证或在线播放；这些必须各自实现和验收，不能由此步骤的资料查询成功推断。使用方法见[在线资料接口](../online.md)。
