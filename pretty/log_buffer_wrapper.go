package pretty

// StyledLogBuffer wraps LogBuffer to provide styled rendering methods for pretty package
type StyledLogBuffer struct {
	*LogBuffer
}

// NewStyledLogBuffer creates a new styled log buffer
func NewStyledLogBuffer(maxSize int) *StyledLogBuffer {
	return &StyledLogBuffer{
		LogBuffer: NewLogBuffer(maxSize),
	}
}

// Render returns a formatted string of the N most recent logs using pretty.Styles
func (slb *StyledLogBuffer) Render(styles Styles, n int, showTime bool) string {
	return RenderLogBuffer(slb.LogBuffer, styles, n, showTime)
}

// FormatStats returns a formatted stats summary using pretty.Styles
func (slb *StyledLogBuffer) FormatStats(styles Styles) string {
	return FormatLogStats(slb.LogBuffer, styles)
}
