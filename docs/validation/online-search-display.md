# 在线搜索与批量展示接口验收

日期：2026-10-10。实现范围为资料查询，不提供在线音频授权。

本轮直接使用正式`online.Client`公网传输验证网易云匿名曲目、专辑、歌手、歌单搜索：`Daft Punk`关键词每类返回2项及下一页游标；批量曲目详情返回2项；公开歌单资料页返回2项、`complete=true`且有下一页。实际运行`TestLiveAnonymousSearchAndBatchedDisplay`通过，耗时7.843秒。没有Cookie、平台凭据、代理或测试Transport，也未下载音频。

接口行为测试覆盖类型分离、跨类型游标拒绝、危险图像地址、非法数量、单次批量请求、重复项、缺失曲目、歌单观测改变后`CURSOR_EXPIRED`、取消、CSRF和其他用户不可见来源；存储测试覆盖完整本人歌单名称匹配和Unicode大小写、键集分页、批量来源顺序及owner隔离。

参考[FeelUOwn网易云适配源码](https://github.com/feeluown/feeluown-netease/blob/master/fuo_netease/api.py)中公开搜索的类型约定；没有复制其实现代码。延续[平台调查](../platform-capabilities.md)已核对的批量详情思路，对正式端点重新进行了实际调用。单批固定最多50项，上游详情缺失显式返回`NOT_FOUND`，不是伪造空曲目。图片仅返回平台HTTPS展示地址，Server不代抓取。

另使用正式匿名传输验证专辑`2462086`、歌手`90513`实体，专辑曲目、歌手专辑、歌手曲目三类关系各取连续两页，每页2项且身份不同；`TestLiveAnonymousEntityDetailsAndRelationshipPaging`通过，耗时8.774秒。旧`/api/album/{id}`返回业务错误-462，未采用；使用实测成功的`/api/v1/album/{id}`。真实验收发现图片CDN域名会轮换；分页观察摘要现排除仅用于展示的封面，仍校验完整曲目集合与资料，避免误报集合过期。

QQ和Apple未新增supported声明；Apple仍要求开发者授权。上游搜索及歌手作品列表没有固定快照；歌单和专辑分页通过每页重新核对完整观察摘要防止混合观察。个人歌单按实时ID键集分页，SourceRef展示使用当前资料，与旧歌单快照修订分开表达。
