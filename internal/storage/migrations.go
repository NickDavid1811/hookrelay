package storage

import (
	"database/sql"
	"fmt"
)

// Schema changes and user_version advance together, including on existing v1 databases.
func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 5 {
		return fmt.Errorf("unsupported schema version %d", version)
	}
	if version < 1 {
		_, err = tx.Exec(`CREATE TABLE events(id TEXT PRIMARY KEY, kind TEXT NOT NULL, body BLOB NOT NULL, created INTEGER NOT NULL);
 CREATE TABLE deliveries(id TEXT PRIMARY KEY, event_id TEXT NOT NULL REFERENCES events(id), status TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, next INTEGER NOT NULL DEFAULT 0, lease INTEGER NOT NULL DEFAULT 0, token TEXT NOT NULL DEFAULT '', http_status INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '');
 CREATE TABLE keys(key TEXT PRIMARY KEY, hash TEXT NOT NULL, event_id TEXT NOT NULL REFERENCES events(id), delivery_id TEXT NOT NULL REFERENCES deliveries(id));
 CREATE TABLE attempts(delivery_id TEXT NOT NULL REFERENCES deliveries(id), number INTEGER NOT NULL, http_status INTEGER NOT NULL, error TEXT NOT NULL, completed INTEGER NOT NULL, PRIMARY KEY(delivery_id,number));
 CREATE INDEX eligible ON deliveries(status,next,lease); PRAGMA user_version=1;`)
		if err != nil {
			return err
		}
	}
	if version < 2 {
		_, err = tx.Exec(`ALTER TABLE deliveries ADD COLUMN cycle INTEGER NOT NULL DEFAULT 1;
 ALTER TABLE deliveries ADD COLUMN cycle_attempts INTEGER NOT NULL DEFAULT 0;
 UPDATE deliveries SET cycle_attempts=attempts;
 ALTER TABLE attempts ADD COLUMN cycle INTEGER NOT NULL DEFAULT 1;
 CREATE TABLE replays(delivery_id TEXT NOT NULL REFERENCES deliveries(id), cycle INTEGER NOT NULL, actor TEXT NOT NULL, reason TEXT NOT NULL, requested INTEGER NOT NULL, PRIMARY KEY(delivery_id,cycle));
 PRAGMA user_version=2;`)
		if err != nil {
			return err
		}
	}
	if version < 3 {
		_, err = tx.Exec(`CREATE TABLE clients(id TEXT PRIMARY KEY,token_hash TEXT UNIQUE,active INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE endpoints(id TEXT PRIMARY KEY,client_id TEXT NOT NULL REFERENCES clients(id),url TEXT NOT NULL,secret_env TEXT NOT NULL,allow_loopback INTEGER NOT NULL DEFAULT 0,active INTEGER NOT NULL DEFAULT 0);
 INSERT INTO clients(id) VALUES('local-client');
 INSERT INTO endpoints(id,client_id,url,secret_env) VALUES('test','local-client','','HOOKRELAY_SECRET');
 ALTER TABLE events ADD COLUMN client_id TEXT NOT NULL DEFAULT 'local-client' REFERENCES clients(id);
 ALTER TABLE deliveries ADD COLUMN endpoint_id TEXT NOT NULL DEFAULT 'test' REFERENCES endpoints(id);
 ALTER TABLE keys RENAME TO keys_v2;
 CREATE TABLE keys(client_id TEXT NOT NULL REFERENCES clients(id),key TEXT NOT NULL,hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES events(id),delivery_id TEXT NOT NULL REFERENCES deliveries(id),PRIMARY KEY(client_id,key));
 INSERT INTO keys SELECT 'local-client',key,hash,event_id,delivery_id FROM keys_v2; DROP TABLE keys_v2;
 CREATE INDEX events_owner ON events(client_id,id);
 CREATE TRIGGER delivery_owner_insert BEFORE INSERT ON deliveries WHEN (SELECT client_id FROM events WHERE id=NEW.event_id)!=(SELECT client_id FROM endpoints WHERE id=NEW.endpoint_id) BEGIN SELECT RAISE(ABORT,'owner mismatch'); END;
 CREATE TRIGGER delivery_owner_update BEFORE UPDATE OF event_id,endpoint_id ON deliveries WHEN (SELECT client_id FROM events WHERE id=NEW.event_id)!=(SELECT client_id FROM endpoints WHERE id=NEW.endpoint_id) BEGIN SELECT RAISE(ABORT,'owner mismatch'); END;
 CREATE TRIGGER event_owner_update BEFORE UPDATE OF client_id ON events WHEN EXISTS(SELECT 1 FROM deliveries d JOIN endpoints ep ON ep.id=d.endpoint_id WHERE d.event_id=OLD.id AND ep.client_id!=NEW.client_id) BEGIN SELECT RAISE(ABORT,'owner mismatch'); END;
 CREATE TRIGGER endpoint_owner_update BEFORE UPDATE OF client_id ON endpoints WHEN NEW.client_id!=OLD.client_id BEGIN SELECT RAISE(ABORT,'endpoint owner immutable'); END;
 PRAGMA user_version=3;`)
		if err != nil {
			return err
		}
	}
	if version < 4 {
		_, err = tx.Exec(`ALTER TABLE deliveries ADD COLUMN terminal_at INTEGER NOT NULL DEFAULT 0;
 UPDATE deliveries SET terminal_at=coalesce((SELECT max(completed) FROM attempts WHERE delivery_id=deliveries.id),(SELECT created FROM events WHERE id=deliveries.event_id)) WHERE status IN ('SUCCESS','DEAD');
 CREATE INDEX deliveries_retention ON deliveries(status,terminal_at,event_id);
 CREATE INDEX deliveries_event ON deliveries(event_id);
 PRAGMA user_version=4;`)
		if err != nil {
			return err
		}
	}
	if version < 5 {
		_, err = tx.Exec(`ALTER TABLE endpoints ADD COLUMN secret TEXT NOT NULL DEFAULT '';
 PRAGMA user_version=5;`)
		if err != nil {
			return err
		}
	}
	var violations int
	if err = tx.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil {
		return err
	}
	if violations != 0 {
		return fmt.Errorf("migration has invalid references")
	}
	return tx.Commit()
}
