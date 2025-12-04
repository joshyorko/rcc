package operations_test

import (
	"bytes"
	"testing"

	"github.com/joshyorko/rcc/hamlet"
	"github.com/joshyorko/rcc/operations"
	"github.com/joshyorko/rcc/pretty"
)

func TestTokenPeriodWorksAsExpected(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	var period *operations.TokenPeriod
	must.Nil(period)
	wont.Panic(func() {
		period.Deadline()
	})
}

func TestDashboardWriterProcessesLines(t *testing.T) {
	must, _ := hamlet.Specifications(t)

	// Create a mock dashboard that captures output
	capturedLines := []string{}
	mockDashboard := &mockDashboard{
		addOutputFunc: func(line string) {
			capturedLines = append(capturedLines, line)
		},
	}

	// Create a buffer to write to
	buf := &bytes.Buffer{}
	writer := operations.NewDashboardWriterForTest(buf, mockDashboard)

	// Write some test data with multiple lines
	testData := "line1\nline2\nline3\n"
	n, err := writer.Write([]byte(testData))

	// Verify write succeeded
	must.Nil(err)
	must.Equal(len(testData), n)

	// Verify data was written to underlying writer
	must.Equal(testData, buf.String())

	// Verify lines were captured by dashboard
	must.Equal(3, len(capturedLines))
	must.Equal("line1", capturedLines[0])
	must.Equal("line2", capturedLines[1])
	must.Equal("line3", capturedLines[2])
}

func TestDashboardWriterHandlesPartialLines(t *testing.T) {
	must, _ := hamlet.Specifications(t)

	capturedLines := []string{}
	mockDashboard := &mockDashboard{
		addOutputFunc: func(line string) {
			capturedLines = append(capturedLines, line)
		},
	}

	buf := &bytes.Buffer{}
	writer := operations.NewDashboardWriterForTest(buf, mockDashboard)

	// Write partial line
	writer.Write([]byte("partial "))
	must.Equal(0, len(capturedLines)) // No complete line yet

	// Complete the line
	writer.Write([]byte("line\n"))
	must.Equal(1, len(capturedLines))
	must.Equal("partial line", capturedLines[0])
}

// mockDashboard is a test implementation of pretty.Dashboard
type mockDashboard struct {
	addOutputFunc func(string)
}

func (m *mockDashboard) Start()                                                {}
func (m *mockDashboard) Stop(success bool)                                     {}
func (m *mockDashboard) Update(state pretty.DashboardState)                    {}
func (m *mockDashboard) SetStep(index int, status pretty.StepStatus, msg string) {}
func (m *mockDashboard) AddOutput(line string) {
	if m.addOutputFunc != nil {
		m.addOutputFunc(line)
	}
}
