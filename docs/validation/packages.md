# 包依赖与构建约定

`src/app` 仅解析进程退出与调用 bootstrap；bootstrap 组装 config、storage、jobs、library、playback 和 api。业务包不导入 api，domain 只含标识和共享错误值。外部插件仅依赖 pluginproto，wire DTO 经边界转换后进入库表。

测试集中于根 tests，通过生产接口验证。不存在子 go.mod 或 src/internal，生产代码不能引用 tests。集成测试默认执行；需外部工具的场景必须清楚报告缺失，不将跳过记为通过。

S-P0-01历史记录：当时Go 1.26.9的Windows amd64与Linux amd64入口构建通过，Linux运行及race尚待对应环境。后续已完成的原生运行与竞争检查见[实施记录](progress.md)；2026-10-10的Linux YAML处理后播放与音频定向补验见[Linux音频记录](configuration-linux-audio.md)，本轮后续完整配置/race结果仍单独确认。ARM64、macOS、Linux浏览器与人工听音不在该结论内。

S-P0-02：入口与组装包可分别构建；domain 不依赖 HTTP/SQL/worker。后续小接口在实际消费时引入。
