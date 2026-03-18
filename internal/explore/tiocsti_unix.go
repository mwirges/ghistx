//go:build unix

package explore

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// InjectTIOCSTI stuffs cmd into the terminal's input buffer via TIOCSTI.
//
// On Linux 6.2+ TIOCSTI requires CAP_SYS_ADMIN; this function returns an
// error in that case so the caller can fall back to printing to stdout.
// Users can set "explore-basic = true" in ~/.histx to skip injection entirely.
func InjectTIOCSTI(cmd string) error {
	fd, err := unix.Open("/dev/tty", unix.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("tiocsti: open /dev/tty: %w", err)
	}
	defer unix.Close(fd)

	for i := 0; i < len(cmd); i++ {
		b := cmd[i]
		_, _, errno := unix.Syscall(
			unix.SYS_IOCTL,
			uintptr(fd),
			unix.TIOCSTI,
			uintptr(unsafe.Pointer(&b)),
		)
		if errno != 0 {
			return fmt.Errorf("tiocsti: ioctl byte %d: %w", i, errno)
		}
	}
	return nil
}
