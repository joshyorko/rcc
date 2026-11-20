package main

type Runner interface {
	Run()
}

type Task struct{}

func (*Task) Run() {
	_ = 1
}
