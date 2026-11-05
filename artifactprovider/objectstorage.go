package artifactprovider

import (
	"context"
	"errors"
	"io"

	"github.com/joshyorko/rcc/environmentartifact"
)

var (
	ErrStorageNotFound  = errors.New("artifact storage object not found")
	ErrStorageConflict  = errors.New("artifact storage conditional write conflict")
	ErrStorageIntegrity = errors.New("artifact storage integrity check failed")
	ErrStorageAmbiguous = errors.New("artifact storage write outcome is unknown")
)

// StorageRevision is an opaque strong backend revision, never a content digest.
type StorageRevision string

type StoredObject struct {
	Digest   environmentartifact.Digest
	Size     int64
	Revision StorageRevision
}

// ObjectStorage is independent of the v1 manifest Provider. Successful GET
// integrity verification requires reading to EOF and checking the read error.
// Closing before verified EOF returns ErrStorageIntegrity.
type ObjectStorage interface {
	PutObject(context.Context, Blob) error
	GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error)
	HeadObject(context.Context, environmentartifact.Digest) (StoredObject, error)
}

// NamedHead records a mutable name's immutable target and opaque revision.
// A revision binds the whole publication, including its unique token.
type NamedHead struct {
	Name     string
	Digest   environmentartifact.Digest
	Revision StorageRevision
}

// NamedHeadStorage atomically creates or advances a named head. Nil expected
// means create-only; a non-nil expected binds both parent digest and revision.
// It does not validate a target's tree or manifest closure.
type NamedHeadStorage interface {
	ResolveHead(context.Context, string) (NamedHead, error)
	CompareAndSwapHead(context.Context, string, *NamedHead, environmentartifact.Digest) (NamedHead, error)
}
