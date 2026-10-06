package artifactprovider

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/joshyorko/rcc/environmentartifact"
)

const maxStorageHeadBytes int64 = 4096

type storageHeadDocument struct {
	SchemaVersion    int                        `json:"schemaVersion"`
	Name             string                     `json:"name"`
	Digest           environmentartifact.Digest `json:"digest"`
	PublicationToken string                     `json:"publicationToken"`
}

func (s *S3Storage) ResolveHead(ctx context.Context, name string) (NamedHead, error) {
	h, _, err := s.readHead(ctx, name)
	return h, err
}

func (s *S3Storage) readHead(ctx context.Context, name string) (NamedHead, []byte, error) {
	if !validStorageName(name) {
		return NamedHead{}, nil, fmt.Errorf("invalid head name: %w", ErrStorageIntegrity)
	}
	r, err := s.request(ctx, http.MethodGet, "heads/"+name, nil)
	if err != nil {
		return NamedHead{}, nil, err
	}
	resp, err := s.do(r)
	if err != nil {
		return NamedHead{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return NamedHead{}, nil, storageStatus(resp.StatusCode)
	}
	revisions := resp.Header.Values("ETag")
	lengths := resp.Header.Values("Content-Length")
	hashes := resp.Header.Values("X-Amz-Meta-Sha256")
	if len(revisions) != 1 || !strongStorageRevision(revisions[0]) || len(lengths) != 1 || len(hashes) != 1 {
		return NamedHead{}, nil, fmt.Errorf("invalid head metadata: %w", ErrStorageIntegrity)
	}
	size, err := strconv.ParseInt(lengths[0], 10, 64)
	if err != nil || size < 1 || size > maxStorageHeadBytes || strconv.FormatInt(size, 10) != lengths[0] || resp.ContentLength >= 0 && resp.ContentLength != size {
		return NamedHead{}, nil, fmt.Errorf("head size out of bounds: %w", ErrStorageIntegrity)
	}
	body, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: resp.Body}, maxStorageHeadBytes+1))
	if err != nil {
		return NamedHead{}, nil, err
	}
	if int64(len(body)) != size || environmentartifact.DigestBytes(body).Hex() != hashes[0] {
		return NamedHead{}, nil, fmt.Errorf("head content mismatch: %w", ErrStorageIntegrity)
	}
	var doc storageHeadDocument
	if err := rejectDuplicateHTTPJSON(body); err != nil {
		return NamedHead{}, nil, fmt.Errorf("invalid head JSON: %w", ErrStorageIntegrity)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return NamedHead{}, nil, fmt.Errorf("invalid head JSON: %w", ErrStorageIntegrity)
	}
	canonical, err := json.Marshal(doc)
	token, tokenErr := hex.DecodeString(doc.PublicationToken)
	if err != nil || !bytes.Equal(canonical, body) || doc.SchemaVersion != 1 || doc.Name != name || !validStorageDigest(doc.Digest) || tokenErr != nil || len(token) != 16 || hex.EncodeToString(token) != doc.PublicationToken {
		return NamedHead{}, nil, fmt.Errorf("invalid head document: %w", ErrStorageIntegrity)
	}
	return NamedHead{Name: name, Digest: doc.Digest, Revision: StorageRevision(revisions[0])}, body, nil
}

func (s *S3Storage) CompareAndSwapHead(ctx context.Context, name string, expected *NamedHead, next environmentartifact.Digest) (NamedHead, error) {
	if !validStorageName(name) || !validStorageDigest(next) {
		return NamedHead{}, fmt.Errorf("invalid head update: %w", ErrStorageIntegrity)
	}
	if err := ctx.Err(); err != nil {
		return NamedHead{}, err
	}
	if expected != nil {
		if expected.Name != name || !validStorageDigest(expected.Digest) || !strongStorageRevision(string(expected.Revision)) {
			return NamedHead{}, fmt.Errorf("invalid expected parent: %w", ErrStorageIntegrity)
		}
		current, err := s.ResolveHead(ctx, name)
		if err != nil {
			if errors.Is(err, ErrStorageNotFound) {
				return NamedHead{}, ErrStorageConflict
			}
			return NamedHead{}, err
		}
		if current != *expected {
			return NamedHead{}, ErrStorageConflict
		}
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return NamedHead{}, errors.New("create head publication token")
	}
	body, err := json.Marshal(storageHeadDocument{SchemaVersion: 1, Name: name, Digest: next, PublicationToken: hex.EncodeToString(token[:])})
	if err != nil || int64(len(body)) > maxStorageHeadBytes {
		return NamedHead{}, fmt.Errorf("head document exceeds bound: %w", ErrStorageIntegrity)
	}
	r, err := s.request(ctx, http.MethodPut, "heads/"+name, bytes.NewReader(body))
	if err != nil {
		return NamedHead{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Amz-Meta-Sha256", environmentartifact.DigestBytes(body).Hex())
	r.Header.Set("X-Amz-Content-Sha256", environmentartifact.DigestBytes(body).Hex())
	if expected == nil {
		r.Header.Set("If-None-Match", "*")
	} else {
		r.Header.Set("If-Match", string(expected.Revision))
	}
	if err := ctx.Err(); err != nil {
		return NamedHead{}, err
	}
	resp, writeErr := s.do(r)
	code := 0
	if resp != nil {
		code = resp.StatusCode
		_ = resp.Body.Close()
	}
	if errors.Is(writeErr, errStorageSigning) {
		return NamedHead{}, writeErr
	}
	uncertain := writeErr != nil || code >= 500 || ctx.Err() != nil
	if writeErr == nil && code != 200 && code != 201 && code != 204 && code != 409 && code != 412 && !uncertain {
		return NamedHead{}, storageStatus(code)
	}
	observeCtx := ctx
	cancel := func() {}
	if uncertain {
		observeCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), s.reconcileTimeout)
	}
	defer cancel()
	observed, readback, observeErr := s.readHead(observeCtx, name)
	if observeErr == nil && bytes.Equal(body, readback) && (expected == nil || observed.Revision != expected.Revision) {
		return observed, nil
	}
	if uncertain {
		return NamedHead{}, errors.Join(ErrStorageAmbiguous, writeErr, observeErr)
	}
	if code == 409 || code == 412 {
		return NamedHead{}, ErrStorageConflict
	}
	// A newer head can replace a successful publication before readback. Its
	// existence cannot prove whether this publication committed, so stay unknown.
	return NamedHead{}, errors.Join(ErrStorageAmbiguous, observeErr)
}

var _ NamedHeadStorage = (*S3Storage)(nil)
