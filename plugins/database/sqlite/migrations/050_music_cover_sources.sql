-- 录音关联变更按曲目重新选封面，先限定该曲目的来源，再查已绑定的封面对象。
CREATE INDEX music_sources_by_track ON music_sources(track_id,owner,id);
