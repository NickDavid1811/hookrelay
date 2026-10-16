package relay

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Only the dedicated Docker target enables this test. Never fill the developer's
// filesystem: even with the flag, require an isolated tmpfs of at most 8 MiB.
func requireBoundedTmpfs(t *testing.T) {
	t.Helper()
	if os.Getenv("HOOKRELAY_TEST_FULL_FS") != "1" {
		t.Skip("run make storage-full-test for isolated filesystem ENOSPC")
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(os.TempDir(), &fs); err != nil {
		t.Fatal(err)
	}
	if fs.Type != 0x01021994 || fs.Blocks*uint64(fs.Bsize) > 8*1024*1024 {
		t.Fatal("refusing to fill a filesystem other than the dedicated <=8 MiB tmpfs")
	}
}

// Write real allocated bytes, not a sparse file. Only successful ENOSPC counts
// as fault injection; permissions and unrelated failures cannot pass this test.
func fillTmpfs(folder string) (release func() error, err error) {
	path := filepath.Join(folder, "disk-pressure")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	block := make([]byte, 64*1024)
	for written := 0; written < 8*1024*1024; {
		n, writeErr := file.Write(block)
		written += n
		if writeErr != nil {
			closeErr := file.Close()
			if !errors.Is(writeErr, syscall.ENOSPC) {
				os.Remove(path)
				return nil, writeErr
			}
			if closeErr != nil {
				os.Remove(path)
				return nil, closeErr
			}
			return func() error { return os.Remove(path) }, nil
		}
	}
	file.Close()
	os.Remove(path)
	return nil, fmt.Errorf("filesystem did not return ENOSPC within safety ceiling")
}
func truncateWAL(t *testing.T, s *Service) {
	t.Helper()
	var busy, log, checkpointed int
	if err := s.DB.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed); err != nil || busy != 0 {
		t.Fatalf("cannot truncate WAL before fault: busy=%d err=%v", busy, err)
	}
}
func TestFilesystemFullPublicationAndRecovery(t *testing.T) {
	requireBoundedTmpfs(t)
	s, path := storageSetup(t)
	original := accepted(t, s, "before-full")
	truncateWAL(t, s)
	before := storedGraph(t, s)
	release, err := fillTmpfs(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { release() })
	_, err = s.application().Publish(context.Background(), "local-client", "during-full", []byte(body))
	requireSQLiteFailure(t, err, 13, 10)
	requireUnavailable(t, publish(s, "during-full", body))
	requireGraph(t, s, before)
	if err = release(); err != nil {
		t.Fatal(err)
	}
	recovered := accepted(t, s, "during-full")
	repeated := accepted(t, s, "during-full")
	if recovered.DeliveryID != repeated.DeliveryID {
		t.Fatal("failed write left a poisoned idempotency key")
	}
	reopenStorage(t, s, path)
	requireDelivery(t, s, original.DeliveryID, "PENDING", 0)
	received := []string{}
	storageReceiver(s, 204, &received, nil)
	for range 2 {
		if _, err = s.dispatchOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	requireDelivery(t, s, original.DeliveryID, "SUCCESS", 1)
	requireDelivery(t, s, recovered.DeliveryID, "SUCCESS", 1)
	if len(received) != 2 {
		t.Fatal("accepted work was lost", len(received))
	}
	requireIntegrity(t, s)
	t.Log("real filesystem ENOSPC: rejected publication rolled back; accepted work survived reopen and delivery")
}
func TestFilesystemFullFinalizationAndRecovery(t *testing.T) {
	requireBoundedTmpfs(t)
	s, path := storageSetup(t)
	receipt := accepted(t, s, "ack-before-full")
	received := []string{}
	var release func() error
	var fillErr error
	var once sync.Once
	storageReceiver(s, 204, &received, func() {
		once.Do(func() {
			// This runs after the claim committed, before the receiver's simulated 204.
			var busy, log, checkpointed int
			fillErr = s.DB.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed)
			if fillErr != nil {
				return
			}
			if busy != 0 {
				fillErr = fmt.Errorf("checkpoint busy")
				return
			}
			release, fillErr = fillTmpfs(filepath.Dir(path))
		})
	})
	t.Cleanup(func() {
		if release != nil {
			release()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.Run(ctx)
	if fillErr != nil {
		t.Fatal(fillErr)
	}
	requireSQLiteFailure(t, err, 13, 10)
	if len(received) != 1 {
		t.Fatal("expected one accepted HTTP request", len(received))
	}
	requireDelivery(t, s, receipt.DeliveryID, "PROCESSING", 0)
	if metricValue(t, s, "hookrelay_delivery_results_total", map[string]string{"result": "SUCCESS"}) != 0 {
		t.Fatal("uncommitted result reported as success")
	}
	var count int
	if err = s.DB.QueryRow("SELECT count(*) FROM attempts").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial attempt committed", count, err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	reopenStorage(t, s, path)
	requireDelivery(t, s, receipt.DeliveryID, "PROCESSING", 0)
	// Advance the persisted lease in this fixture; SIGKILL tests cover the real delay.
	mustExec(t, s.DB, "UPDATE deliveries SET lease=0 WHERE id=?", receipt.DeliveryID)
	if _, err = s.dispatchOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || received[0] != receipt.DeliveryID || received[1] != receipt.DeliveryID {
		t.Fatal("duplicate recovery changed the delivery identifier")
	}
	requireDelivery(t, s, receipt.DeliveryID, "SUCCESS", 1)
	requireIntegrity(t, s)
	t.Log("real filesystem ENOSPC: finalization failed after receiver ACK; worker stopped; reopen and lease recovery preserved delivery ID")
}
