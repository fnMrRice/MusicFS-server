-- 只保存扫描时从当前文件读取的网易云曲目编号。NULL 表示同一资产的标签矛盾，新版本资产重新取证。
CREATE TABLE music_netease_asset_ids (
    asset_id TEXT PRIMARY KEY REFERENCES source_assets(id) ON DELETE CASCADE,
    netease_id TEXT CHECK (netease_id IS NULL OR length(netease_id)>0)
);
