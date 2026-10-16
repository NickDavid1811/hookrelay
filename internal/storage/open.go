package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite"
)

// Open initializes a SQLite writer database connection configured with WAL mode, synchronous FULL,
// busy timeout of 5000ms, and runs all pending schema migrations (up to Schema v5).
func Open(path string) (*sql.DB, error) {
	dsn, err := connectionDSN(path, false)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	// Check structural corruption before schema migration or journal-mode writes.
	// Full integrity/index consistency is checked separately by backup validation.
	var check string
	if err = db.QueryRow("PRAGMA quick_check(1)").Scan(&check); err != nil {
		db.Close()
		return nil, fmt.Errorf("SQLite quick_check failed: %w", err)
	}
	if check != "ok" {
		db.Close()
		return nil, errors.New("SQLite structural integrity check failed")
	}

	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// OpenReader initializes a concurrent read-only connection pool using WAL mode and mode=ro.
func OpenReader(path string) (*sql.DB, error) {
	dsn, err := connectionDSN(path, true)
	if err != nil {
		return nil, err
	}
	rdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conns := runtime.NumCPU() * 2
	if conns > 32 {
		conns = 32
	}
	if conns < 2 {
		conns = 2
	}
	rdb.SetMaxOpenConns(conns)
	rdb.SetMaxIdleConns(conns)
	if _, err = rdb.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		rdb.Close()
		return nil, err
	}
	return rdb, nil
}

// OpenPool opens both a serialized writer and a concurrent read-only reader connection pool.
func OpenPool(path string) (writer *sql.DB, reader *sql.DB, err error) {
	writer, err = Open(path)
	if err != nil {
		return nil, nil, err
	}
	if path == ":memory:" {
		return writer, writer, nil
	}
	reader, err = OpenReader(path)
	if err != nil {
		writer.Close()
		return nil, nil, err
	}
	return writer, reader, nil
}

// DSN pragmas are run by the driver on every physical connection, including replacements.
func connectionDSN(path string, readOnly bool) (string, error) {
	var u *url.URL
	var err error
	if path == ":memory:" {
		u = &url.URL{Scheme: "file", Opaque: ":memory:"}
	} else if strings.HasPrefix(path, "file:") {
		u, err = url.Parse(path)
	} else {
		var absolute string
		absolute, err = filepath.Abs(path)
		if err == nil {
			u = &url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
		}
	}
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	if readOnly {
		q.Set("mode", "ro")
	} else {
		q.Add("_pragma", "synchronous(FULL)")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
