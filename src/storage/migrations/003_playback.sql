CREATE TABLE playback_sessions(id TEXT PRIMARY KEY,auth_session TEXT NOT NULL,user_id TEXT NOT NULL,description BLOB NOT NULL,state TEXT NOT NULL,expires_at TEXT NOT NULL);
