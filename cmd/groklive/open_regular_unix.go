//go:build unix

package main

import (
	"os"
	"syscall"
)

// openFileReadNoBlock opens path for reading with O_NONBLOCK so a FIFO does not
// hang the process in Open before mode validation (Pro R46 P2).
func openFileReadNoBlock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
