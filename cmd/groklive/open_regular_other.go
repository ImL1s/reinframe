//go:build !unix

package main

import "os"

// openFileReadNoBlock opens path for reading. On non-unix platforms there is no
// portable O_NONBLOCK FIFO semantics; call sites still Stat/Lstat for IsRegular
// before open and re-check on the FD (Pro R46 P2).
func openFileReadNoBlock(path string) (*os.File, error) {
	return os.Open(path)
}
