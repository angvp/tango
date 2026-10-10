//go:build darwin

package shellcore

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY returns the two ends of a new pseudo-terminal.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal available: %v", err)
	}
	fd := master.Fd()
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, unix.TIOCPTYGRANT, 0); errno != 0 {
		master.Close()
		t.Fatal(errno)
	}
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, unix.TIOCPTYUNLK, 0); errno != 0 {
		master.Close()
		t.Fatal(errno)
	}
	var name [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		master.Close()
		t.Fatal(errno)
	}
	end := 0
	for end < len(name) && name[end] != 0 {
		end++
	}
	slave, err = os.OpenFile(string(name[:end]), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	return master, slave
}
