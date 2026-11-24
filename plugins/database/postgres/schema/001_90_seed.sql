-- 创建初始管理员时由用户触发器同步生成收藏和最近播放的修订。
INSERT INTO users(id,role) VALUES ('admin','admin');
INSERT INTO library_revision(singleton,revision) VALUES (1,0);
