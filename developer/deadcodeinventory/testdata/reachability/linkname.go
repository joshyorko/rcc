package main

import _ "unsafe"

//go:linkname LinkedOnly
func LinkedOnly() {}
