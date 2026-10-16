package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func requireIOContainer(t *testing.T) {
	t.Helper()
	if os.Getenv("HOOKRELAY_TEST_STORAGE_FAULTS") != "1" {
		t.Skip("run make storage-io-test for isolated EIO and interrupted WAL tests")
	}
	requireBoundedTmpfs(t)
}

// A restrictive, test-only seccomp filter returns EIO for WAL writes or syncs.
// It is installed in a disposable child process because filters cannot be removed.
// TSYNC covers worker goroutines regardless of the OS thread chosen by Go.
func failWALSyscalls(t *testing.T, path, mode string) {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	var descriptors []int
	for _, entry := range entries {
		n, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		target, err := os.Readlink("/proc/self/fd/" + entry.Name())
		if err != nil || target != path+"-wal" {
			continue
		}
		flags, err := unix.FcntlInt(uintptr(n), unix.F_GETFL, 0)
		if err == nil && flags&unix.O_ACCMODE == unix.O_RDWR {
			descriptors = append(descriptors, n)
		}
	}
	if len(descriptors) == 0 {
		t.Fatal("writer WAL descriptor not found")
	}
	numbers := []uint32{uint32(unix.SYS_PWRITE64)}
	if mode == "sync" {
		numbers = []uint32{uint32(unix.SYS_FSYNC), uint32(unix.SYS_FDATASYNC)}
	}
	// seccomp_data.nr at offset 0, args[0] (fd) low word at offset 16.
	filters := []unix.SockFilter{{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16}}
	// SQLite may open the WAL read/write even through a read-only SQL pool.
	// Match every descriptor for this WAL, independent of discovery order.
	for _, fd := range descriptors {
		filters = append(filters,
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: uint32(fd), Jf: uint8(2*len(numbers) + 2)},
			unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0})
		for _, number := range numbers {
			filters = append(filters,
				unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: number, Jf: 1},
				unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EIO)})
		}
		filters = append(filters, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
	}
	filters = append(filters, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
	program := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]}
	if err = unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	result, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	runtime.KeepAlive(filters)
	if errno != 0 || result != 0 {
		t.Fatalf("cannot install EIO filter: result=%d errno=%v", result, errno)
	}
}

func childStorage(t *testing.T, path string) *Service {
	t.Helper()
	db, reader, err := OpenPool(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close(); db.Close() })
	s := &Service{DB: db, ReadDB: reader}
	cfg := Config{Clients: []ClientConfig{{ID: "local-client", TokenEnv: "TEST_TOKEN", Endpoints: []EndpointConfig{{ID: "test", URL: "http://127.0.0.1:1", SecretEnv: "TEST_SECRET", AllowLoopback: true}}}}}
	if err = s.ApplyConfig(cfg, func(name string) string {
		if name == "TEST_TOKEN" {
			return testToken
		}
		return testSecret
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

// Invoked only by the parent tests, with a path inside their guarded tmpfs.
func TestFilesystemFaultChild(t *testing.T) {
	requireIOContainer(t)
	mode := os.Getenv("HOOKRELAY_FAULT_MODE")
	if mode == "" {
		t.Skip("child process only")
	}
	path := os.Getenv("HOOKRELAY_FAULT_DB")
	s := childStorage(t, path)
	if mode == "uncommitted" {
		mustExec(t, s.DB, "PRAGMA cache_size=1")
		mustExec(t, s.DB, "PRAGMA wal_autocheckpoint=0")
		truncateWAL(t, s)
		prefix := accepted(t, s, "wal-accepted-prefix")
		tx, err := s.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		// Spill an uncommitted transaction into WAL. This fixture is deliberately
		// larger than the public API payload ceiling to force dirty-page eviction.
		for _, q := range []string{
			"INSERT INTO events(id,kind,body,created,client_id) VALUES('uncommitted','test',zeroblob(131072),0,'local-client')",
			"INSERT INTO deliveries(id,event_id,status,endpoint_id) VALUES('uncommitted','uncommitted','PENDING','test')",
			"INSERT INTO keys VALUES('local-client','uncommitted','hash','uncommitted','uncommitted')",
		} {
			if _, err = tx.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		marker, err := json.Marshal(prefix)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path+".ready.tmp", marker, 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(path+".ready.tmp", path+".ready"); err != nil {
			t.Fatal(err)
		}
		select {} // Parent SIGKILLs this process, without defers, commit or checkpoint.
	}
	parts := strings.Split(mode, "-")
	if len(parts) != 2 {
		t.Fatal("invalid fault mode")
	}
	syncMode, stage := parts[0], parts[1]
	expected := 10 | (3 << 8) // SQLITE_IOERR_WRITE
	if syncMode == "sync" {
		expected = 10 | (4 << 8)
	} // SQLITE_IOERR_FSYNC
	if stage == "publish" {
		truncateWAL(t, s)
		failWALSyscalls(t, path, syncMode)
		_, err := s.application().Publish(context.Background(), "local-client", "uncertain", []byte(body))
		requireExactIOError(t, err, expected)
		requireUnavailable(t, publish(s, "uncertain", body))
		if metricValue(t, s, "hookrelay_publications_total", map[string]string{"result": "new"}) != 0 {
			t.Fatal("failed commit reported as accepted")
		}
	} else if stage == "finalize" {
		received := []string{}
		storageReceiver(s, 204, &received, func() { truncateWAL(t, s); failWALSyscalls(t, path, syncMode) })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := s.Run(ctx)
		requireExactIOError(t, err, expected)
		if len(received) != 1 {
			t.Fatal("expected exactly one outbound acknowledgement")
		}
		if metricValue(t, s, "hookrelay_delivery_results_total", map[string]string{"result": "SUCCESS"}) != 0 {
			t.Fatal("failed finalization reported as committed")
		}
	} else {
		t.Fatal("unknown stage")
	}
}

func requireExactIOError(t *testing.T, err error, want int) {
	t.Helper()
	requireSQLiteFailure(t, err, 10)
	var failure interface{ Code() int }
	// The underlying driver error is preserved by the application path.
	if !errors.As(err, &failure) || failure.Code() != want {
		t.Fatalf("want SQLite I/O code %d, got %v", want, err)
	}
}

func faultChild(ctx context.Context, path, mode string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFilesystemFaultChild$", "-test.v", "-test.timeout=15s")
	cmd.Env = append(os.Environ(), "HOOKRELAY_FAULT_MODE="+mode, "HOOKRELAY_FAULT_DB="+path)
	return cmd
}
func TestFilesystemIOFailures(t *testing.T) {
	requireIOContainer(t)
	for _, mode := range []string{"write-publish", "sync-publish", "write-finalize", "sync-finalize"} {
		t.Run(mode, func(t *testing.T) {
			s, path := storageSetup(t)
			seed := accepted(t, s, "accepted-before-fault")
			s.ReadDB.Close()
			s.DB.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := faultChild(ctx, path, mode)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fault child failed: %v\n%s", err, output)
			}
			t.Log(string(output))
			reopenStorage(t, s, path)
			requireIntegrity(t, s)
			received := []string{}
			storageReceiver(s, 204, &received, nil)
			if strings.HasSuffix(mode, "publish") {
				requireDelivery(t, s, seed.DeliveryID, "PENDING", 0)
				retried := accepted(t, s, "uncertain")
				again := accepted(t, s, "uncertain")
				if retried.DeliveryID != again.DeliveryID {
					t.Fatal("uncertain commit retry duplicated publication")
				}
				for _, table := range []string{"events", "deliveries", "keys"} {
					var count int
					if err = s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 2 {
						t.Fatalf("partial publication in %s: %d %v", table, count, err)
					}
				}
				for range 2 {
					if _, err = s.dispatchOne(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				requireDelivery(t, s, seed.DeliveryID, "SUCCESS", 1)
				requireDelivery(t, s, retried.DeliveryID, "SUCCESS", 1)
			} else {
				result, err := s.application().Delivery(context.Background(), "local-client", seed.DeliveryID)
				if err != nil {
					t.Fatal(err)
				}
				// A failed sync is an uncertain commit: on reopen, either a complete
				// finalization or an outstanding claim is valid, never a partial history.
				history, err := s.application().Attempts(context.Background(), "local-client", seed.DeliveryID, 0)
				if err != nil {
					t.Fatal(err)
				}
				switch result.Status {
				case "PROCESSING":
					if result.Attempts != 0 || len(history) != 0 {
						t.Fatal("partial finalization survived")
					}
					mustExec(t, s.DB, "UPDATE deliveries SET lease=0 WHERE id=?", seed.DeliveryID)
					if _, err = s.dispatchOne(context.Background()); err != nil {
						t.Fatal(err)
					}
					if len(received) != 1 || received[0] != seed.DeliveryID {
						t.Fatal("lease recovery changed delivery ID")
					}
				case "SUCCESS":
					if result.Attempts != 1 || len(history) != 1 {
						t.Fatal("committed state missing its history")
					}
				default:
					t.Fatal("unexpected recovered state", result.Status)
				}
				requireDelivery(t, s, seed.DeliveryID, "SUCCESS", 1)
			}
			requireIntegrity(t, s)
		})
	}
}

func TestFilesystemInterruptedWAL(t *testing.T) {
	requireIOContainer(t)
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial-tail-%t", partial), func(t *testing.T) {
			s, path := storageSetup(t)
			seed := accepted(t, s, "durable-prefix")
			s.ReadDB.Close()
			s.DB.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := faultChild(ctx, path, "uncommitted")
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			killed := false
			defer func() {
				if !killed {
					cmd.Process.Kill()
					<-done
				}
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(path + ".ready"); err == nil {
					break
				}
				select {
				case err := <-done:
					killed = true
					t.Fatalf("child exited before barrier: %v %s", err, output.String())
				default:
				}
				if time.Now().After(deadline) {
					t.Fatal("uncommitted barrier timeout")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			<-done
			killed = true
			wal, err := os.ReadFile(path + "-wal")
			if err != nil {
				t.Fatal(err)
			}
			if len(wal) < 32 {
				t.Fatal("no WAL header")
			}
			page := int(binary.BigEndian.Uint32(wal[8:12]))
			if page == 1 {
				page = 65536
			}
			frame := 24 + page
			if page < 512 || len(wal) < 32+frame {
				t.Fatal("uncommitted transaction did not spill WAL frames")
			}
			if (len(wal)-32)%frame != 0 {
				t.Fatal("unexpected incomplete WAL before truncation")
			}
			hasCommit := false
			lastCommit := false
			for offset := 32; offset+frame <= len(wal); offset += frame {
				lastCommit = binary.BigEndian.Uint32(wal[offset+4:offset+8]) != 0
				hasCommit = hasCommit || lastCommit
			}
			if !hasCommit || lastCommit {
				t.Fatal("fixture must have a committed prefix followed by uncommitted frames")
			}

			if partial {
				if err = os.Truncate(path+"-wal", int64(len(wal)-13)); err != nil {
					t.Fatal(err)
				}
			}
			marker, err := os.ReadFile(path + ".ready")
			if err != nil {
				t.Fatal(err)
			}
			var prefix Result
			if err = json.Unmarshal(marker, &prefix); err != nil {
				t.Fatal(err)
			}
			reopenStorage(t, s, path)
			requireDelivery(t, s, seed.DeliveryID, "PENDING", 0)
			requireDelivery(t, s, prefix.DeliveryID, "PENDING", 0)
			repeated := accepted(t, s, "wal-accepted-prefix")
			if repeated.EventID != prefix.EventID || repeated.DeliveryID != prefix.DeliveryID {
				t.Fatal("lost accepted WAL prefix")
			}
			for _, table := range []string{"events", "deliveries", "keys"} {
				var count int
				if err = s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 2 {
					t.Fatalf("uncommitted writes survived in %s: %d %v", table, count, err)
				}
			}
			requireIntegrity(t, s)
			received := []string{}
			storageReceiver(s, 204, &received, nil)
			for range 2 {
				if _, err = s.dispatchOne(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			requireDelivery(t, s, seed.DeliveryID, "SUCCESS", 1)
			requireDelivery(t, s, prefix.DeliveryID, "SUCCESS", 1)
		})
	}
}
