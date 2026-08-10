//go:build !unix

package main

import "fmt"

func mkfifo(path string) error {
	return fmt.Errorf("mkfifo not supported")
}
