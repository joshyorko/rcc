package main

func main() {
	Live()
	Shared()
}

func Live() {}

func UsedByTestOnly() {}

func NeverUsed() {
	deadHelper()
	Shared()
}

func deadHelper() {}

func Shared() {}
