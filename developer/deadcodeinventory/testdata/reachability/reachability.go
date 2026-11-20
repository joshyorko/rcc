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
	_ = Live
}

func deadHelper() {}

func Shared() {}
