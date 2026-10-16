package storage

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPoolInitializesEveryPhysicalConnection(t *testing.T) {
	db, reader, err := OpenPool(filepath.Join(t.TempDir(), "special?name#data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer reader.Close()
	ctx := context.Background()
	a, err := reader.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := reader.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for _, conn := range []*sql.Conn{a, b} {
		var timeout, fk int
		if err = conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatal(err)
		}
		if timeout != 5000 || fk != 1 {
			t.Fatalf("timeout=%d foreign_keys=%d", timeout, fk)
		}
		if _, err = conn.ExecContext(ctx, "INSERT INTO clients(id) VALUES('not-writable')"); err == nil {
			t.Fatal("reader accepted a write")
		}
	}
	db.SetMaxIdleConns(0) // Force the next writer to be a replacement physical connection.
	var fk, syncMode, timeout int
	writer, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk)
	writer.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&syncMode)
	writer.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout)
	if fk != 1 || syncMode != 2 || timeout != 5000 {
		t.Fatalf("replacement writer: fk=%d synchronous=%d timeout=%d", fk, syncMode, timeout)
	}
}
func TestMemoryPoolSharesDatabase(t *testing.T) {
	writer, reader, err := OpenPool(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if writer != reader {
		t.Fatal("in-memory database split across independent pools")
	}
	var schema int
	if err = reader.QueryRow("PRAGMA user_version").Scan(&schema); err != nil || schema != 5 {
		t.Fatal(schema, err)
	}
}

func TestOpenRejectsCorruptIndexBeforeMigration(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprintf("schema-%d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "corrupt.db")
			db, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if version == 4 {
				if _, err = db.Exec("ALTER TABLE endpoints DROP COLUMN secret; PRAGMA user_version=4"); err != nil {
					t.Fatal(err)
				}
			}
			var pageSize, root int64
			if err = db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT rootpage FROM sqlite_master WHERE name='eligible'").Scan(&root); err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			file, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			// The index root page is neither the header nor a foreign-key parent.
			if _, err = file.WriteAt([]byte{0}, (root-1)*pageSize); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			opened, err := Open(path)
			if opened != nil {
				opened.Close()
			}
			if err == nil {
				t.Fatal("accepted database with a corrupt delivery index")
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("modified the corrupt database before rejecting it")
			}
		})
	}
}
