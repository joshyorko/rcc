package main

import _ "unsafe"

//go:linkname LinkedOnly
func LinkedOnly() {}

//go:linkname localLinked github.com/joshyorko/rcc/developer/deadcodeinventory/testdata/reachability.RemoteLinked
func localLinked()

func RemoteLinked() {}
