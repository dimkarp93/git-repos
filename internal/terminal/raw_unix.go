//go:build linux || darwin

package terminal

import (
	"os"
	"syscall"
	"unsafe"
)

func ioctl(fd uintptr, req uintptr, arg *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(arg)))
	if errno != 0 {
		return errno
	}
	return nil
}

func MakeRaw(f *os.File) (restore func(), err error) {
	var saved syscall.Termios
	if err := ioctl(f.Fd(), getTermios, &saved); err != nil {
		return nil, err
	}
	raw := saved
	raw.Iflag &^= syscall.ICRNL | syscall.INLCR | syscall.IGNCR | syscall.IXON | syscall.ISTRIP | syscall.BRKINT
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(f.Fd(), setTermios, &raw); err != nil {
		return nil, err
	}
	return func() { ioctl(f.Fd(), setTermios, &saved) }, nil
}
