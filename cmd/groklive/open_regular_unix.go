//go:build unix

package main

import (
	"os"
	"syscall"
)

// openFileReadNoBlock opens path for reading with O_NONBLOCK|O_NOFOLLOW so a
// FIFO does not hang Open and a symlink planted after Lstat cannot redirect
// the open (Pro R46/R49 P2).
func openFileReadNoBlock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
