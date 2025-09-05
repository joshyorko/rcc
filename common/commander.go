package common

import "strings"

type Commander struct {
	command []string
}

func (it *Commander) Option(name, value string) *Commander {
	value = strings.TrimSpace(value)
	if len(value) > 0 {
		it.command = append(it.command, name, value)
	}
	return it
}

func (it *Commander) ConditionalFlag(condition bool, details ...string) *Commander {
	if condition {
		it.command = append(it.command, details...)
	}
	return it
}

func (it *Commander) CLI() []string {
	return it.command
}

func NewCommander(parts ...string) *Commander {
	return &Commander{parts}
}
