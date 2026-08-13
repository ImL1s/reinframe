//go:build unix

package main

import (
	"os"
	"syscall"
)

// openFileReadNoBlock opens path for reading with O_NONBLOCK|O_NOFOLLOW so a
// FIFO does not hang Open and a symlink planted after Lstat cannot redirect
// the open (Pro R46/R49 P2). Evidence/binding artifacts only.
func openFileReadNoBlock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}

// openFileReadNoBlockFollow opens path for reading with O_NONBLOCK only so
// legitimate executable symlinks still resolve (Pro R50 P2). Callers must
// re-check the opened FD with SameFile against a pre-open Stat.
func openFileReadNoBlockFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
