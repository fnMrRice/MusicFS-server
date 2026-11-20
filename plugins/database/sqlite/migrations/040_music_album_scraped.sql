-- 专辑刮削资料按字段保存来源和内容，生命周期跟随音乐专辑。
CREATE TABLE music_album_scraped_fields (
 album_id TEXT NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
 field TEXT NOT NULL CHECK(field IN ('albumDetails','albumTracks','artwork')),
 document TEXT NOT NULL CHECK(json_valid(document)),
 PRIMARY KEY(album_id,field)
);
