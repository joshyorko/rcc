package main

import "testing"

func TestUsedByTestOnly(t *testing.T) {
	t.Helper()
	UsedByTestOnly()
}
