package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"hookrelay/internal/core"
)

func (s *SQLite) Configure(clients []core.ClientSetup) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE endpoints SET active=0 WHERE secret = ''; UPDATE clients SET active=0,token_hash=NULL"); err != nil {
		return err
	}
	for _, c := range clients {
		if _, err = tx.Exec("INSERT INTO clients(id,token_hash,active) VALUES(?,?,1) ON CONFLICT(id) DO UPDATE SET token_hash=excluded.token_hash,active=1", c.ID, c.TokenHash); err != nil {
			return err
		}
		for _, e := range c.Endpoints {
			var storedOwner, url, secretEnv string
			var loop bool
			err = tx.QueryRow("SELECT client_id,url,secret_env,allow_loopback FROM endpoints WHERE id=?", e.ID).Scan(&storedOwner, &url, &secretEnv, &loop)
			if err == nil && (storedOwner != c.ID || (url != "" && (url != e.URL || secretEnv != e.SecretEnv || loop != e.AllowLoopback))) {
				return fmt.Errorf("endpoint %s is immutable; use a new ID", e.ID)
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if _, err = tx.Exec("INSERT INTO endpoints(id,client_id,url,secret_env,allow_loopback,active) VALUES(?,?,?,?,?,1) ON CONFLICT(id) DO UPDATE SET url=excluded.url,secret_env=excluded.secret_env,allow_loopback=excluded.allow_loopback,active=1", e.ID, c.ID, e.URL, e.SecretEnv, e.AllowLoopback); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}
