package common

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/robocorp/rcc/fail"
)

const (
	StatusOk      = `ok`
	StatusWarning = `warning`
	StatusFail    = `fail`
	StatusFatal   = `fatal`
)

type Diagnoser func(category uint64, status, link, form string, details ...interface{})

func (it Diagnoser) Ok(category uint64, form string, details ...interface{}) {
	it(category, StatusOk, "", form, details...)
}

func (it Diagnoser) Warning(category uint64, link, form string, details ...interface{}) {
	it(category, StatusWarning, link, form, details...)
}

func (it Diagnoser) Fail(category uint64, link, form string, details ...interface{}) {
	it(category, StatusFail, link, form, details...)
}

func (it Diagnoser) Fatal(category uint64, link, form string, details ...interface{}) {
	it(category, StatusFatal, link, form, details...)
}

type DiagnosticStatus struct {
	Details map[string]string  `json:"details"`
	Checks  []*DiagnosticCheck `json:"checks"`
}

type DiagnosticCheck struct {
	Type     string `json:"type"`
	Category uint64 `json:"category"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Link     string `json:"url"`
}

func (it *DiagnosticStatus) check(category uint64, kind, status, message, link string) {
	it.Checks = append(it.Checks, &DiagnosticCheck{
		Type:     kind,
		Category: category,
		Status:   status,
		Message:  message,
		Link:     link,
	})
}

func (it *DiagnosticStatus) Diagnose(kind string) Diagnoser {
	return func(category uint64, status, link, form string, details ...interface{}) {
		it.check(category, kind, status, fmt.Sprintf(form, details...), link)
	}
}

func (it *DiagnosticStatus) Counts() (fatal, fail, warning, ok int) {
	result := make(map[string]int)
	for _, check := range it.Checks {
		result[check.Status] += 1
	}
	return result[StatusFatal], result[StatusFail], result[StatusWarning], result[StatusOk]
}

func (it *DiagnosticStatus) AsJson() (string, error) {
	body, err := json.MarshalIndent(it, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func IsInsideProductHome(location string) (_ bool, err error) {
	defer fail.Around(&err)

	candidate, err := filepath.Abs(location)
	fail.On(err != nil, "Failed to get absolute path to %q, reason: %v", location, err)

	rchome, err := filepath.Abs(Product.Home())
	fail.On(err != nil, "Failed to get absolute path to %s, reason: %v", Product.HomeVariable(), err)

	for len(rchome) <= len(candidate) {
		if rchome == candidate {
			return true, nil
		}
		candidate = filepath.Dir(candidate)
	}

	return false, nil
}
