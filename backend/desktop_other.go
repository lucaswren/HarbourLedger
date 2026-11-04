//go:build !windows

package main

import "fmt"

func prepareDesktopLog(string)        {}
func showDesktopError(message string) { fmt.Println(message) }
func acquireDesktop(string) (func(), bool, error) {
	return func() {}, false, fmt.Errorf("客戶端僅支援 Windows")
}
func runDesktop(string, string) error { return fmt.Errorf("客戶端僅支援 Windows") }
