package pathlib_test

import (
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/pathlib"
)

func TestCalculateMD5OfFiles(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	digest, err := pathlib.Md5("testdata/missing")
	wont.Nil(err)
	must.Equal("", digest)

	digest, err = pathlib.Md5("testdata/empty")
	must.Nil(err)
	must.Equal("d41d8cd98f00b204e9800998ecf8427e", digest)

	digest, err = pathlib.Md5("testdata/hello.txt")
	must.Nil(err)
	must.Equal("746308829575e17c3331bbcb00c0898b", digest)
}
