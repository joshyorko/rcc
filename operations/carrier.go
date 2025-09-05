package operations

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
)

const (
	scissors = `---8x---`
)

func FindExecutable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return "", err
	}
	self, err = filepath.Abs(self)
	if err != nil {
		return "", err
	}
	return self, nil
}

func SelfCopy(target string) error {
	self, err := FindExecutable()
	if err != nil {
		return err
	}
	source, err := os.Open(self)
	if err != nil {
		return err
	}
	defer source.Close()

	sink, err := os.OpenFile(target, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0o755)
	if err != nil {
		return err
	}
	defer sink.Close()
	size, err := io.Copy(sink, source)
	if err != nil {
		return err
	}
	common.Debug("Copied %q to %q as size of %d bytes.", self, target, size)
	return nil
}

func SelfAppend(target, payload string) error {
	size, ok := pathlib.Size(target)
	if !ok {
		return fmt.Errorf("Could not get size of %q.", target)
	}
	source, err := os.Open(payload)
	if err != nil {
		return err
	}
	defer source.Close()
	sink, err := os.OpenFile(target, os.O_WRONLY|os.O_APPEND, 0o755)
	if err != nil {
		return err
	}
	defer sink.Close()
	_, err = sink.Write([]byte(scissors))
	if err != nil {
		return err
	}
	_, err = io.Copy(sink, source)
	if err != nil {
		return err
	}
	err = binary.Write(sink, binary.LittleEndian, size)
	if err != nil {
		return err
	}
	return nil
}

func HasPayload(filename string) (bool, error) {
	reader, err := PayloadReaderAt(filename)
	if err != nil {
		return false, err
	}
	reader.Close()
	return true, nil
}

func IsCarrier() (bool, error) {
	carrier, err := FindExecutable()
	if err != nil {
		return false, err
	}
	return HasPayload(carrier)
}

type ReaderCloserAt interface {
	io.ReaderAt
	io.Closer
	Limit() int64
}

type carrier struct {
	source *os.File
	offset int64
	limit  int64
}

func (it *carrier) Limit() int64 {
	return it.limit
}

func (it *carrier) ReadAt(target []byte, offset int64) (int, error) {
	_, err := it.source.Seek(it.offset+offset, 0)
	if err != nil {
		return 0, err
	}
	return it.source.Read(target)
}

func (it *carrier) Close() error {
	return it.source.Close()
}

func PayloadReaderAt(filename string) (ReaderCloserAt, error) {
	size, ok := pathlib.Size(filename)
	if !ok {
		return nil, fmt.Errorf("Could not get size of %q.", filename)
	}
	source, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	_, err = source.Seek(size-8, 0)
	if err != nil {
		source.Close()
		return nil, err
	}
	var offset int64
	err = binary.Read(source, binary.LittleEndian, &offset)
	if err != nil {
		source.Close()
		return nil, err
	}
	if offset < 0 || size <= offset {
		source.Close()
		return nil, fmt.Errorf("%q has no carrier payload.", filename)
	}
	_, err = source.Seek(offset, 0)
	if err != nil {
		source.Close()
		return nil, err
	}
	marker := make([]byte, 8)
	count, err := source.Read(marker)
	if err != nil {
		source.Close()
		return nil, err
	}
	if count != 8 || string(marker) != scissors {
		source.Close()
		return nil, fmt.Errorf("%q has no carrier payload.", filename)
	}
	return &carrier{source, offset + 8, size - offset - 16}, nil
}
