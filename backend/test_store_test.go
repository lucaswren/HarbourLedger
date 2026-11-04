package main

import "testing"

// Existing import tests use a fictional, explicitly configured company.
func newTestStore(t *testing.T) (*Store, error) {
	t.Helper()
	s, err := openStore(t.TempDir())
	if err == nil {
		_, err = s.settings(Record{"company": "EXAMPLE SERVICES LIMITED"})
	}
	return s, err
}
