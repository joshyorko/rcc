package common

import (
	"fmt"
)

type ExitCode struct {
	Code    int
	Message string
}

func (it ExitCode) ShowMessage() {
	Log(it.Message)
}

func Exit(code int, format string, rest ...interface{}) {
	message := format
	if len(rest) > 0 {
		message = fmt.Sprintf(format, rest...)
	}
	panic(ExitCode{
		Code:    code,
		Message: message,
	})
}
