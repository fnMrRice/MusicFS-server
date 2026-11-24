CREATE TABLE matching_revisions (
    owner TEXT COLLATE "C" PRIMARY KEY,
    revision BIGINT NOT NULL CHECK(revision>=0)
);

CREATE TABLE recordings (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    created_at TEXT COLLATE "C" NOT NULL,
    updated_at TEXT COLLATE "C" NOT NULL
);

CREATE TABLE recording_source_links (
    owner TEXT COLLATE "C" NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    recording_id TEXT COLLATE "C" REFERENCES recordings(id),
    decision TEXT COLLATE "C" NOT NULL,
    revision BIGINT NOT NULL,
    PRIMARY KEY(owner,source_id)
);

CREATE TABLE match_exclusions (
    owner TEXT COLLATE "C" NOT NULL,
    left_source TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    right_source TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    excluded BIGINT NOT NULL CHECK(excluded IN(0,1)),
    revision BIGINT NOT NULL,
    PRIMARY KEY(owner,left_source,right_source),
    CHECK(left_source<right_source)
);

CREATE TABLE link_changes (
    sequence BIGINT NOT NULL,
    owner TEXT COLLATE "C" NOT NULL,
    revision BIGINT NOT NULL,
    operation_id TEXT COLLATE "C" NOT NULL,
    document TEXT COLLATE "C" NOT NULL,
    PRIMARY KEY(owner,sequence)
);

CREATE TABLE matching_candidates (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    left_source TEXT COLLATE "C" NOT NULL,
    right_source TEXT COLLATE "C" NOT NULL,
    left_asset TEXT COLLATE "C" NOT NULL,
    right_asset TEXT COLLATE "C" NOT NULL,
    score BIGINT NOT NULL,
    status TEXT COLLATE "C" NOT NULL,
    binding_digest TEXT COLLATE "C" NOT NULL,
    document TEXT COLLATE "C" NOT NULL,
    created_at TEXT COLLATE "C" NOT NULL,
    CHECK(left_source<right_source)
);

CREATE TABLE media_content_objects (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    asset_id TEXT COLLATE "C" NOT NULL REFERENCES source_assets(id),
    publication_id TEXT COLLATE "C" NOT NULL UNIQUE REFERENCES media_publications(id),
    state TEXT COLLATE "C" NOT NULL CHECK(state IN ('available','unavailable')),
    document TEXT COLLATE "C" NOT NULL,
    created_at TEXT COLLATE "C" NOT NULL
);

CREATE TABLE media_selection_revisions (
    owner TEXT COLLATE "C" NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    revision BIGINT NOT NULL CHECK(revision>=0),
    PRIMARY KEY(owner,source_id)
);

CREATE TABLE media_content_bindings (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    object_id TEXT COLLATE "C" NOT NULL REFERENCES media_content_objects(id),
    asset_id TEXT COLLATE "C" NOT NULL REFERENCES source_assets(id),
    field TEXT COLLATE "C" NOT NULL,
    language TEXT COLLATE "C" NOT NULL,
    current BIGINT NOT NULL CHECK(current IN (0,1)),
    document TEXT COLLATE "C" NOT NULL,
    created_at TEXT COLLATE "C" NOT NULL
);

CREATE TABLE media_content_slots (
    owner TEXT COLLATE "C" NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    field TEXT COLLATE "C" NOT NULL,
    language TEXT COLLATE "C" NOT NULL,
    binding_id TEXT COLLATE "C" NOT NULL REFERENCES media_content_bindings(id),
    manual_locked BIGINT NOT NULL CHECK(manual_locked IN (0,1)),
    PRIMARY KEY(owner,source_id,field,language)
);

CREATE TABLE media_selection_changes (
    owner TEXT COLLATE "C" NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    revision BIGINT NOT NULL,
    operation TEXT COLLATE "C" NOT NULL,
    field TEXT COLLATE "C" NOT NULL,
    language TEXT COLLATE "C" NOT NULL,
    previous_binding_id TEXT COLLATE "C",
    next_binding_id TEXT COLLATE "C",
    manual_locked BIGINT NOT NULL,
    created_at TEXT COLLATE "C" NOT NULL,
    PRIMARY KEY(owner,source_id,revision,field,language)
);

CREATE TABLE media_garbage (
    id TEXT COLLATE "C" PRIMARY KEY,
    publication_id TEXT COLLATE "C" NOT NULL UNIQUE REFERENCES media_publications(id),
    object_id TEXT COLLATE "C" NOT NULL UNIQUE,
    state TEXT COLLATE "C" NOT NULL CHECK(state IN ('claimed','deleted')),
    created_at TEXT COLLATE "C" NOT NULL,
    updated_at TEXT COLLATE "C" NOT NULL
);

CREATE TABLE media_backups (
    id TEXT COLLATE "C" PRIMARY KEY,
    database_path TEXT COLLATE "C" NOT NULL UNIQUE,
    state TEXT COLLATE "C" NOT NULL CHECK(state IN ('preparing','prepared','finished')),
    object_count BIGINT NOT NULL DEFAULT 0,
    missing_count BIGINT NOT NULL DEFAULT 0,
    created_at TEXT COLLATE "C" NOT NULL,
    updated_at TEXT COLLATE "C" NOT NULL
);

CREATE TABLE media_backup_objects (
    backup_id TEXT COLLATE "C" NOT NULL REFERENCES media_backups(id),
    object_id TEXT COLLATE "C" NOT NULL,
    owner TEXT COLLATE "C" NOT NULL,
    publication_id TEXT COLLATE "C" NOT NULL,
    state TEXT COLLATE "C" NOT NULL CHECK(state IN ('pending','copied','missing')),
    document TEXT COLLATE "C" NOT NULL,
    PRIMARY KEY(backup_id,object_id)
);

CREATE TABLE enrichment_jobs (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    state TEXT COLLATE "C" NOT NULL CHECK(state IN ('queued','running','completed','failed','cancelled')),
    request TEXT COLLATE "C" NOT NULL,
    created_at TEXT COLLATE "C" NOT NULL,
    updated_at TEXT COLLATE "C" NOT NULL
);

CREATE TABLE enrichment_targets (
    job_id TEXT COLLATE "C" NOT NULL REFERENCES enrichment_jobs(id),
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    owner TEXT COLLATE "C" NOT NULL,
    state TEXT COLLATE "C" NOT NULL CHECK(state IN ('queued','running','completed','failed','cancelled')),
    generation BIGINT NOT NULL CHECK(generation>0),
    target TEXT COLLATE "C" NOT NULL,
    issues TEXT COLLATE "C" NOT NULL,
    failure_code TEXT COLLATE "C",
    preview_bytes BIGINT NOT NULL DEFAULT 0 CHECK(preview_bytes>=0),
    candidate_count BIGINT NOT NULL DEFAULT 0 CHECK(candidate_count>=0),
    PRIMARY KEY(job_id,source_id)
);

CREATE TABLE enrichment_candidates (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    job_id TEXT COLLATE "C" NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL,
    generation BIGINT NOT NULL,
    object_id TEXT COLLATE "C" REFERENCES media_content_objects(id),
    expires_at TEXT COLLATE "C" NOT NULL,
    document TEXT COLLATE "C" NOT NULL,
    FOREIGN KEY(job_id,source_id) REFERENCES enrichment_targets(job_id,source_id)
);

CREATE TABLE writeback_jobs (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    state TEXT COLLATE "C" NOT NULL CHECK(state IN ('queued','running','recovery','failed','succeeded','cancelled')),
    old_size BIGINT NOT NULL CHECK(old_size>0),
    work TEXT COLLATE "C" NOT NULL,
    error TEXT COLLATE "C",
    result TEXT COLLATE "C",
    commit_result TEXT COLLATE "C",
    created_at TEXT COLLATE "C" NOT NULL,
    updated_at TEXT COLLATE "C" NOT NULL
);

CREATE TABLE writeback_operations (
    owner TEXT COLLATE "C" NOT NULL,
    key TEXT COLLATE "C" NOT NULL,
    request_digest TEXT COLLATE "C" NOT NULL,
    binding_digest TEXT COLLATE "C" NOT NULL,
    job_id TEXT COLLATE "C" NOT NULL REFERENCES writeback_jobs(id),
    expires_at TEXT COLLATE "C" NOT NULL,
    PRIMARY KEY(owner,key)
);

CREATE TABLE source_albums (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    provider_id TEXT COLLATE "C" NOT NULL,
    name TEXT COLLATE "C" NOT NULL,
    revision BIGINT NOT NULL CHECK(revision>=1),
    created_at TEXT COLLATE "C" NOT NULL,
    updated_at TEXT COLLATE "C" NOT NULL
);

CREATE TABLE source_album_members (
    album_id TEXT COLLATE "C" NOT NULL REFERENCES source_albums(id),
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    position BIGINT NOT NULL CHECK(position>=0),
    PRIMARY KEY(album_id,source_id),
    UNIQUE(album_id,position)
);

CREATE TABLE releases (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    name TEXT COLLATE "C" NOT NULL,
    revision BIGINT NOT NULL CHECK(revision>=1),
    created_at TEXT COLLATE "C" NOT NULL,
    updated_at TEXT COLLATE "C" NOT NULL
);

CREATE TABLE release_albums (
    release_id TEXT COLLATE "C" NOT NULL REFERENCES releases(id),
    album_id TEXT COLLATE "C" NOT NULL REFERENCES source_albums(id),
    position BIGINT NOT NULL CHECK(position>=0),
    PRIMARY KEY(release_id,album_id),
    UNIQUE(release_id,position)
);

CREATE TABLE album_cover_bindings (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    target_kind TEXT COLLATE "C" NOT NULL CHECK(target_kind IN ('sourceAlbum','release')),
    target_id TEXT COLLATE "C" NOT NULL,
    object_id TEXT COLLATE "C" NOT NULL REFERENCES media_content_objects(id),
    target_revision BIGINT NOT NULL,
    source_id TEXT COLLATE "C" NOT NULL REFERENCES tracks(id),
    asset_id TEXT COLLATE "C" NOT NULL REFERENCES source_assets(id),
    asset_version TEXT COLLATE "C" NOT NULL,
    created_at TEXT COLLATE "C" NOT NULL
);

CREATE TABLE album_cover_heads (
    owner TEXT COLLATE "C" NOT NULL,
    target_kind TEXT COLLATE "C" NOT NULL CHECK(target_kind IN ('sourceAlbum','release')),
    target_id TEXT COLLATE "C" NOT NULL,
    revision BIGINT NOT NULL CHECK(revision>=1),
    binding_id TEXT COLLATE "C" NOT NULL REFERENCES album_cover_bindings(id),
    manual_locked BIGINT NOT NULL CHECK(manual_locked IN (0,1)),
    PRIMARY KEY(owner,target_kind,target_id)
);

CREATE TABLE album_changes (
    sequence BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    target_kind TEXT COLLATE "C" NOT NULL,
    target_id TEXT COLLATE "C" NOT NULL,
    document TEXT COLLATE "C" NOT NULL
);

CREATE TABLE media_derived_objects (
    id TEXT COLLATE "C" PRIMARY KEY,
    owner TEXT COLLATE "C" NOT NULL,
    publication_id TEXT COLLATE "C" NOT NULL UNIQUE REFERENCES media_publications(id),
    relationship_key TEXT COLLATE "C" NOT NULL,
    transform TEXT COLLATE "C" NOT NULL,
    parent_object_id TEXT COLLATE "C",
    job_id TEXT COLLATE "C",
    source_id TEXT COLLATE "C",
    generation BIGINT NOT NULL,
    state TEXT COLLATE "C" NOT NULL CHECK (state IN ('available','unavailable')),
    document TEXT COLLATE "C" NOT NULL,
    created_at TEXT COLLATE "C" NOT NULL
);
