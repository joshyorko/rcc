package conda

import (
	"testing"

	"github.com/robocorp/rcc/hamlet"
)

func TestCanValidateLocations(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	must_be.True(validateLocations(map[string]string{"TMP": "C:\\TMP"}))
	must_be.True(validateLocations(map[string]string{"TMP": "C:\\TMP/space"}))
	wont_be.True(validateLocations(map[string]string{"TMP": "C:\\TMP SPACE\\"}))
	wont_be.True(validateLocations(map[string]string{"TMP": "C:\\TMP\tTAB\\"}))

	must_be.True(validateLocations(map[string]string{"TMP": "C:\\Users\\Hulk\\AppData\\local\\robocorp\\"}))
	must_be.True(validateLocations(map[string]string{"TMP": "C:\\Users\\Dr.Strange\\AppData\\local\\robocorp\\"}))
	wont_be.True(validateLocations(map[string]string{"TMP": "C:\\Users\\Black Widow\\AppData\\local\\robocorp\\"}))
	wont_be.True(validateLocations(map[string]string{"TMP": "C:\\Users\\Åland\\AppData\\local\\robocorp\\"}))

	must_be.True(validateLocations(map[string]string{"TMP": "C:\\ProgramData\\Temp\\rc_032a54\\AppData\\Local\\robocorp\\"}))
}
