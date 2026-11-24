CREATE TABLE musicfs_plugin_values (
    plugin TEXT COLLATE "C" NOT NULL,
    instance TEXT COLLATE "C" NOT NULL,
    key TEXT COLLATE "C" NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    value BYTEA NOT NULL,
    PRIMARY KEY (plugin, instance, key)
);
