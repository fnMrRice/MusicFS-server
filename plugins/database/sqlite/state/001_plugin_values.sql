CREATE TABLE IF NOT EXISTS plugin_values (
    scope TEXT NOT NULL,
    key TEXT NOT NULL,
    revision INTEGER NOT NULL,
    value BLOB NOT NULL,
    PRIMARY KEY (scope, key)
);
PRAGMA user_version = 1;
