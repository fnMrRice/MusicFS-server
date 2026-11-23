-- 空任务编号表示尚未为此歌单或关联安排曲库入库，启动时由已配置的实例领取。
ALTER TABLE remote_playlists ADD COLUMN catalog_job_id TEXT REFERENCES remote_sync_jobs(id);
ALTER TABLE playlist_links ADD COLUMN catalog_job_id TEXT REFERENCES remote_sync_jobs(id);
