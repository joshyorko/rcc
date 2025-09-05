package pretty

import "fmt"

const (
	Escape = 0x1b
)

func csif(form string, values ...interface{}) string {
	return csi(fmt.Sprintf(form, values...))
}

func csi(value string) string {
	return fmt.Sprintf("%c[%s", Escape, value)
}
