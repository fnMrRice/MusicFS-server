CREATE TABLE music_tracks (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    title TEXT COLLATE "C" NOT NULL CHECK(length(trim(title))>0),
    cover_path TEXT COLLATE "C",
    genre TEXT COLLATE "C",
    year BIGINT,
    rating BIGINT CHECK(rating IS NULL OR (rating BETWEEN 1 AND 5)),
    origin TEXT COLLATE "C" NOT NULL DEFAULT 'manual' CHECK(origin IN ('manual','source','recording')),
    metadata_source_id TEXT COLLATE "C",
    metadata_locked BIGINT NOT NULL DEFAULT 0 CHECK(metadata_locked IN (0,1)),
    revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>=1),
    created_at TEXT COLLATE "C" NOT NULL DEFAULT(musicfs_timestamp()),
    updated_at TEXT COLLATE "C" NOT NULL DEFAULT(musicfs_timestamp()),
    localized_titles TEXT COLLATE "C" NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(localized_titles::jsonb)='array'),
    scraper_placeholder BIGINT NOT NULL DEFAULT 0 CHECK(scraper_placeholder IN (0,1)),
    UNIQUE(owner,id)
);

CREATE TABLE music_artists (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    name TEXT COLLATE "C" NOT NULL CHECK(length(trim(name))>0),
    names_revision BIGINT NOT NULL DEFAULT 1,
    UNIQUE(owner,name)
);

CREATE TABLE music_track_artists (
    track_id TEXT COLLATE "C" NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
    artist_id TEXT COLLATE "C" NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
    position BIGINT NOT NULL CHECK(position>=0),
    PRIMARY KEY(track_id,artist_id),
    UNIQUE(track_id,position)
);

CREATE TABLE music_albums (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    name TEXT COLLATE "C" NOT NULL CHECK(length(trim(name))>0),
    external_id TEXT COLLATE "C",
    is_external BIGINT NOT NULL DEFAULT 0 CHECK(is_external IN (0,1)),
    cover_path TEXT COLLATE "C",
    publisher TEXT COLLATE "C",
    release_date TEXT COLLATE "C",
    origin TEXT COLLATE "C" NOT NULL DEFAULT 'manual' CHECK(origin IN ('manual','source','cd','tag')),
    tag_provider TEXT COLLATE "C",
    tag_name TEXT COLLATE "C",
    metadata_locked BIGINT NOT NULL DEFAULT 0 CHECK(metadata_locked IN (0,1)),
    revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>=1),
    original_name TEXT COLLATE "C" NOT NULL DEFAULT '',
    source_links TEXT COLLATE "C" NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(source_links::jsonb)='array'),
    UNIQUE(owner,tag_provider,tag_name)
);

CREATE TABLE music_manual_album_tracks (
    album_id TEXT COLLATE "C" NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    track_id TEXT COLLATE "C" NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
    position BIGINT NOT NULL CHECK(position>=0),
    PRIMARY KEY(album_id,track_id)
);

CREATE TABLE music_source_providers (
    provider_instance_id TEXT COLLATE "C" PRIMARY KEY,
    plugin_id TEXT COLLATE "C" NOT NULL CHECK(length(plugin_id)>0)
);

CREATE TABLE music_sources (
    id TEXT COLLATE "C" PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    owner TEXT COLLATE "C" NOT NULL,
    track_id TEXT COLLATE "C" NOT NULL,
    plugin_id TEXT COLLATE "C",
    provider_instance_id TEXT COLLATE "C" NOT NULL,
    external_id TEXT COLLATE "C" NOT NULL,
    created_at TEXT COLLATE "C" NOT NULL DEFAULT(musicfs_timestamp()),
    updated_at TEXT COLLATE "C" NOT NULL DEFAULT(musicfs_timestamp()),
    FOREIGN KEY(owner,track_id) REFERENCES music_tracks(owner,id),
    UNIQUE(owner,provider_instance_id,external_id)
);

CREATE TABLE music_album_source_refs (
    album_id TEXT COLLATE "C" NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    origin_id TEXT COLLATE "C" NOT NULL,
    position BIGINT NOT NULL CHECK(position>=0),
    PRIMARY KEY(album_id,origin_id,source_id)
);

CREATE TABLE music_playlist_entries (
    owner TEXT COLLATE "C" NOT NULL,
    playlist_id TEXT COLLATE "C" NOT NULL,
    binding_id TEXT COLLATE "C" NOT NULL,
    entry_id TEXT COLLATE "C" NOT NULL,
    track_id TEXT COLLATE "C" NOT NULL,
    forced_source_id TEXT COLLATE "C",
    position BIGINT NOT NULL CHECK(position>=0),
    source_contribution BIGINT NOT NULL DEFAULT 0 CHECK(source_contribution IN (0,1) AND (source_contribution=0 OR forced_source_id IS NOT NULL)),
    created_at TEXT COLLATE "C" NOT NULL DEFAULT(musicfs_timestamp()),
    updated_at TEXT COLLATE "C" NOT NULL DEFAULT(musicfs_timestamp()),
    play_count BIGINT NOT NULL DEFAULT 0 CHECK(play_count>=0),
    PRIMARY KEY(owner,playlist_id,binding_id,entry_id),
    FOREIGN KEY(owner,track_id) REFERENCES music_tracks(owner,id)
);

CREATE TABLE content_history_pending (
    owner TEXT COLLATE "C" NOT NULL,
    type_id TEXT COLLATE "C" NOT NULL,
    event_id TEXT COLLATE "C" NOT NULL,
    work_id TEXT COLLATE "C" NOT NULL,
    work_title TEXT COLLATE "C" NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL,
    track_title TEXT COLLATE "C" NOT NULL,
    recorded_at TEXT COLLATE "C" NOT NULL,
    PRIMARY KEY(owner,type_id,event_id)
);

CREATE TABLE personal_collection_revisions (
    owner TEXT COLLATE "C" NOT NULL,
    kind TEXT COLLATE "C" NOT NULL,
    revision BIGINT NOT NULL DEFAULT 0,
    created_at TEXT COLLATE "C" NOT NULL DEFAULT(musicfs_timestamp()),
    updated_at TEXT COLLATE "C" NOT NULL DEFAULT(musicfs_timestamp()),
    PRIMARY KEY(owner,kind)
);

CREATE TABLE music_album_scraped_fields (
    album_id TEXT COLLATE "C" NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    field TEXT COLLATE "C" NOT NULL CHECK(field IN ('albumDetails','albumTracks','artwork')),
    document TEXT COLLATE "C" NOT NULL CHECK(document::jsonb IS NOT NULL),
    PRIMARY KEY(album_id,field)
);

CREATE TABLE music_album_scraped_track_refs (
    album_id TEXT COLLATE "C" NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    slot TEXT COLLATE "C" NOT NULL,
    track_id TEXT COLLATE "C" NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
    plugin_id TEXT COLLATE "C" NOT NULL,
    release_id TEXT COLLATE "C" NOT NULL,
    position BIGINT NOT NULL CHECK(position>=0),
    external_id TEXT COLLATE "C" NOT NULL DEFAULT '',
    PRIMARY KEY(album_id,slot),
    UNIQUE(album_id,track_id)
);

CREATE TABLE music_artist_localized_names (
    artist_id TEXT COLLATE "C" NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
    language TEXT COLLATE "C" NOT NULL,
    name TEXT COLLATE "C" NOT NULL,
    PRIMARY KEY(artist_id,language,name)
);

CREATE TABLE music_album_merged_slots (
    album_id TEXT COLLATE "C" NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    plugin_id TEXT COLLATE "C" NOT NULL,
    release_id TEXT COLLATE "C" NOT NULL,
    slot_key TEXT COLLATE "C" NOT NULL,
    track_id TEXT COLLATE "C" NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
    preferred BIGINT NOT NULL CHECK(preferred IN(0,1)),
    PRIMARY KEY(album_id,plugin_id,release_id,slot_key)
);

CREATE TABLE recommendation_pages (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    owner_id TEXT COLLATE "C" NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope TEXT COLLATE "C" NOT NULL,
    server_day TEXT COLLATE "C" NOT NULL,
    successful BIGINT NOT NULL CHECK (successful IN (0, 1)),
    sources_json TEXT COLLATE "C" NOT NULL
);

CREATE TABLE playlist_links (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    platform TEXT COLLATE "C" NOT NULL,
    instance_id TEXT COLLATE "C" NOT NULL,
    account_id TEXT COLLATE "C" NOT NULL,
    external_id TEXT COLLATE "C" NOT NULL,
    personal_playlist_id TEXT COLLATE "C" REFERENCES personal_playlists(id) ON DELETE CASCADE,
    playlist_id TEXT COLLATE "C" GENERATED ALWAYS AS (coalesce(personal_playlist_id,'favorites')) STORED,
    catalog_job_id TEXT COLLATE "C" REFERENCES remote_sync_jobs(id),
    UNIQUE(platform,external_id)
);

CREATE TABLE playlist_link_additions (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    link_id BIGINT NOT NULL REFERENCES playlist_links(id) ON DELETE CASCADE,
    track_id TEXT COLLATE "C" NOT NULL,
    attempts BIGINT NOT NULL DEFAULT 0,
    retry_at BIGINT NOT NULL DEFAULT 0,
    last_error TEXT COLLATE "C" NOT NULL DEFAULT '',
    UNIQUE(link_id,track_id)
);

CREATE TABLE online_downloads (
    id TEXT COLLATE "C" PRIMARY KEY,
    lookup_key TEXT COLLATE "C" NOT NULL,
    owner TEXT COLLATE "C" NOT NULL,
    plugin_id TEXT COLLATE "C" NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    document TEXT COLLATE "C" NOT NULL CHECK(document::jsonb IS NOT NULL)
);

CREATE TABLE music_netease_asset_ids (
    asset_id TEXT COLLATE "C" PRIMARY KEY REFERENCES source_assets(id) ON DELETE CASCADE,
    netease_id TEXT COLLATE "C" CHECK (netease_id IS NULL OR length(netease_id)>0)
);
