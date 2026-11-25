package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

// madeEnd is the name made adds last to the folder it watches: every change do made is recorded
// before it, so once it is read nothing do did is still to come.
const madeEnd = "made-watch-end"

// statusPending is the NTSTATUS an OVERLAPPED holds while its read is pending.
const statusPending = 0x103

// watching is what made's reads hand the kernel: the buffer it writes the records to and the
// OVERLAPPED it writes the read's status to. made holds it on the heap, through t's cleanup,
// until the handle is closed: the kernel writes to both while a read is pending, across do, and
// Go may move a goroutine's stack.
type watching struct {
	buf [16 << 10]uint32 // 64 KiB, the most a watch's buffer may be; uint32 keeps it aligned
	o   syscall.Overlapped
}

// made runs do and answers the names, relative to dir, added in dir or below it while do ran, in
// the order the system recorded them, removed since or not: what do made, for an instant or to
// keep. It reads the system's own record of the folder's changes (ReadDirectoryChangesW), so the
// code under test needs no seam to say what it made. The first read is queued before do runs,
// which starts the record, and each read after it gets what was recorded since the one before.
// lock and confine each keep a copy, by the standard library alone, which lock's tests are held
// to.
func made(t *testing.T, dir string, do func()) []string {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(name, syscall.FILE_LIST_DIRECTORY, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil,
		syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		t.Fatalf("open %s to watch it: %v", dir, err)
	}
	w := new(watching)
	t.Cleanup(func() {
		// A read a t.Fatal left pending is cancelled, and waited on with no clock, before the
		// handle closes: the kernel is done with w before it may be freed. A hang is go test's
		// -timeout to end. The cancel's answer decides nothing: the wait is the check.
		if w.o.Internal == statusPending {
			_ = syscall.CancelIoEx(h, &w.o)
			if _, err := syscall.WaitForSingleObject(h, syscall.INFINITE); err != nil {
				t.Error(err)
			}
		}
		if err := syscall.CloseHandle(h); err != nil {
			t.Error(err)
		}
		runtime.KeepAlive(w)
	})
	buf, o := w.buf[:], &w.o
	if err := read(h, o, buf); err != nil {
		t.Fatalf("watch %s: %v", dir, err)
	}
	do()
	end := filepath.Join(dir, madeEnd)
	if err := os.WriteFile(end, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(end); err != nil {
			t.Error(err)
		}
	}()
	var names []string
	for {
		n, err := readDone(h, o)
		if err != nil {
			t.Fatalf("watch %s: %v", dir, err)
		}
		got, ended := added(buf[:n/4])
		names = append(names, got...)
		if ended {
			return names
		}
		if err := read(h, o, buf); err != nil {
			t.Fatalf("watch %s: %v", dir, err)
		}
	}
}

// read queues a read of h's record of names made, removed and renamed, in its folder and below it,
// into buf. With no event in o, h itself is signalled when the read is done.
//
//nolint:gosec // G103: buf is handed to the system, which writes the records there
func read(h syscall.Handle, o *syscall.Overlapped, buf []uint32) error {
	*o = syscall.Overlapped{}
	return syscall.ReadDirectoryChanges(h, (*byte)(unsafe.Pointer(&buf[0])), uint32(4*len(buf)), true,
		syscall.FILE_NOTIFY_CHANGE_FILE_NAME|syscall.FILE_NOTIFY_CHANGE_DIR_NAME, nil, o, 0)
}

// readDone waits up to ten seconds for the read queued on h to be done, and answers the bytes of
// records it wrote, as GetOverlappedResult does: o's Internal is the read's status and its
// InternalHigh the bytes. None means the record was lost, more having changed than buf holds.
func readDone(h syscall.Handle, o *syscall.Overlapped) (int, error) {
	switch ev, err := syscall.WaitForSingleObject(h, 10_000); {
	case err != nil:
		return 0, err
	case ev != syscall.WAIT_OBJECT_0:
		return 0, fmt.Errorf("the read was not done in ten seconds (%d)", ev)
	case o.Internal != 0:
		return 0, fmt.Errorf("the read failed with NTSTATUS %#x", o.Internal)
	case o.InternalHigh == 0:
		return 0, fmt.Errorf("the record was lost: more changed than the buffer holds")
	}
	return int(o.InternalHigh), nil
}

// added is the names records says were added, made or renamed in, but madeEnd, and whether madeEnd
// was.
//
//nolint:gosec // G103: the records are read as the system lays them out, each DWORD-aligned
func added(records []uint32) (names []string, ended bool) {
	for off := uint32(0); ; {
		rec := (*syscall.FileNotifyInformation)(unsafe.Pointer(&records[off/4]))
		name := syscall.UTF16ToString(unsafe.Slice(&rec.FileName, rec.FileNameLength/2))
		switch {
		case rec.Action != syscall.FILE_ACTION_ADDED && rec.Action != syscall.FILE_ACTION_RENAMED_NEW_NAME:
		case name == madeEnd:
			ended = true
		default:
			names = append(names, name)
		}
		if rec.NextEntryOffset == 0 {
			return names, ended
		}
		off += rec.NextEntryOffset
	}
}
