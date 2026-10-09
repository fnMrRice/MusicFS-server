CREATE TABLE config_operations (
    id TEXT PRIMARY KEY,
    config_path TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    digest TEXT NOT NULL,
    previous_revision TEXT NOT NULL,
    next_revision TEXT NOT NULL,
    plugin_id TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
    status TEXT NOT NULL CHECK(status IN ('prepared', 'saved', 'applied', 'notSaved', 'superseded')),
    UNIQUE(config_path, idempotency_key)
);
