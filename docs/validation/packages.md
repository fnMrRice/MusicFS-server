# 包依赖与构建约定

`src/app` 仅解析进程退出与调用 bootstrap；bootstrap 组装 config、storage、jobs、library、playback 和 api。业务包不导入 api，domain 只含标识和共享错误值。外部插件仅依赖 pluginproto，wire DTO 经边界转换后进入库表。

测试集中于根 tests，通过生产接口验证。不存在子 go.mod 或 src/internal，生产代码不能引用 tests。集成测试默认执行；需外部工具的场景必须清楚报告缺失，不将跳过记为通过。

S-P0-01：Go 1.26.9 的 Windows amd64 与 Linux amd64 入口构建通过。Linux 运行、ARM64、race 检查尚待对应环境。

S-P0-02：入口与组装包可分别构建；domain 不依赖 HTTP/SQL/worker。后续小接口在实际消费时引入。
