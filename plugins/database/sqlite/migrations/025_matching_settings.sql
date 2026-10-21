CREATE TABLE plugin_settings (
 owner TEXT NOT NULL, plugin_id TEXT NOT NULL, revision INTEGER NOT NULL,
 document BLOB NOT NULL, PRIMARY KEY(owner,plugin_id)
);
CREATE TABLE matching_runs (
 owner TEXT PRIMARY KEY, document BLOB NOT NULL
);
CREATE TABLE matching_action_receipts (
 owner TEXT NOT NULL, idempotency_key TEXT NOT NULL, document BLOB NOT NULL,
 PRIMARY KEY(owner,idempotency_key)
);
