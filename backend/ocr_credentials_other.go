//go:build !windows

package main

func localOCRCredentials() ocrCredentialStore { return nil }
