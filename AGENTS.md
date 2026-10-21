# MusicFS 工作约定

- 架构事实来源：[MusicFS 架构](https://github.com/fnMrRice/MusicFS/blob/main/docs/architecture.md)。总体架构与共享图示由 MusicFS 管理；本仓库拥有 OpenAPI、事件协议、Provider SDK 与数据库迁移。
- 移动文件使用 `git mv`，不以重写后删除代替。
- 源文件最好不超过 400 行，超出时应按模块拆分；因格式化或确实无法避免时可适当放宽。
- 宿主测试统一放根 `tests/`，按 unit、integration、contract、e2e 分类，`src/` 不放测试文件、辅助代码或夹具。通过真实业务接口验证行为，不为测试导出内部实现；测试程序入口放 `tests/testapps/`。SDK 与插件独立模块保留各自的测试边界。
- 未明确要求推送时，不进行 GPG 签名；明确要求推送时，先对尚未推送的旧提交补签，再推送。
- 用户明确要求：画图优先使用 PlantUML（.puml）；界面线框优先用 PlantUML Salt。保留可编辑源文件；渲染图需与源文件一致。
- 中文文档与说明；程序日志和输出使用英文。Shell 注释与日志使用英文。
- 不将设计草案或线框描述为已实现功能。技术选型未经验证时明确标记。
- 各仓库均保持私有；凭据不提交到 Git。
