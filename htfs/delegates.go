package htfs

import (
	"compress/gzip"
	"io"
	"os"
	"runtime"
	"sync"

	"github.com/joshyorko/rcc/fail"
	"github.com/klauspost/compress/zstd"
)

// Platform-specific encoder options.
// Windows uses faster settings to compensate for slower pure Go encoder performance.
// Linux/macOS use better compression since encoding is already fast.
func encoderOptions() []zstd.EOption {
	baseOpts := []zstd.EOption{
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithEncoderConcurrency(1), // Single-threaded per encoder (we parallelize at file level)
		zstd.WithEncoderCRC(false),     // Skip CRC (we verify hash separately)
	}

	if runtime.GOOS == "windows" {
		// Windows: optimize for encoding speed
		// - Smaller window reduces memory operations
		// - Skip entropy compression for faster encoding
		return append(baseOpts,
			zstd.WithWindowSize(1<<18),          // 256KB window
			zstd.WithNoEntropyCompression(true), // Skip Huffman coding
		)
	}

	// Linux/macOS: better compression (encoder is fast anyway)
	return append(baseOpts,
		zstd.WithWindowSize(1<<20), // 1MB window for better compression
	)
}

// Magic byte constants for compression format detection.
// Note: Go doesn't support const for byte slices, but these values are
// immutable format identifiers and should never be modified.
const (
	// gzip magic bytes (RFC 1952)
	gzipMagic0 = 0x1f
	gzipMagic1 = 0x8b
	// zstd frame magic (RFC 8878)
	zstdMagic0 = 0x28
	zstdMagic1 = 0xb5
	zstdMagic2 = 0x2f
	zstdMagic3 = 0xfd
)

// Decoder pool to eliminate per-file allocation overhead.
// Creating a new zstd.Decoder for each file is expensive (~50μs).
// With pooling, we reuse decoders via Reset() (~1μs).
var zstdDecoderPool = sync.Pool{
	New: func() interface{} {
		// Create decoder with nil reader - will be Reset() before use
		decoder, err := zstd.NewReader(nil,
			zstd.WithDecoderConcurrency(1),   // Single-threaded per decoder
			zstd.WithDecoderLowmem(false),    // Trade memory for speed
			zstd.WithDecoderMaxWindow(1<<30), // Support large windows
		)
		if err != nil {
			// Should never happen with nil reader
			return nil
		}
		return decoder
	},
}

// Encoder pool to eliminate per-file allocation overhead.
// Creating a new zstd.Encoder for each file is expensive (~50μs).
// With pooling, we reuse encoders via Reset() (~1μs).
var zstdEncoderPool = sync.Pool{
	New: func() interface{} {
		// Create encoder with nil writer - will be Reset() before use
		// Uses platform-specific options (see encoderOptions())
		encoder, err := zstd.NewWriter(nil, encoderOptions()...)
		if err != nil {
			return nil
		}
		return encoder
	},
}

// GetPooledEncoder obtains a zstd encoder from the pool and resets it for the given writer.
// Returns the encoder and a cleanup function that returns it to the pool.
// The cleanup function MUST be called after Close() to return the encoder to the pool.
func GetPooledEncoder(w io.Writer) (*zstd.Encoder, func(), error) {
	pooled := zstdEncoderPool.Get()
	if pooled == nil {
		// Pool creation failed, fall back to new encoder with matching options
		encoder, err := zstd.NewWriter(w, encoderOptions()...)
		if err != nil {
			return nil, nil, err
		}
		// No-op cleanup - caller handles Close() which releases resources
		return encoder, func() {}, nil
	}

	encoder := pooled.(*zstd.Encoder)
	encoder.Reset(w)

	return encoder, func() {
		// Return to pool for reuse
		zstdEncoderPool.Put(encoder)
	}, nil
}

// getPooledDecoder obtains a zstd decoder from the pool and resets it for the given reader.
// Returns the decoder and a cleanup function that returns it to the pool.
func getPooledDecoder(r io.Reader) (*zstd.Decoder, func(), error) {
	pooled := zstdDecoderPool.Get()
	if pooled == nil {
		// Pool creation failed, fall back to new decoder
		decoder, err := zstd.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		return decoder, func() { decoder.Close() }, nil
	}

	decoder := pooled.(*zstd.Decoder)
	if err := decoder.Reset(r); err != nil {
		// Reset failed, close and create new
		decoder.Close()
		newDecoder, err := zstd.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		return newDecoder, func() { newDecoder.Close() }, nil
	}

	return decoder, func() {
		// IOReadCloser is not closed here - the caller handles that
		zstdDecoderPool.Put(decoder)
	}, nil
}

// Buffer pool for efficient I/O operations.
// Default io.Copy uses 32KB buffers. We use 256KB for better SSD performance.
const copyBufferSize = 256 * 1024 // 256KB - optimal for modern SSDs

var copyBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, copyBufferSize)
		return &buf
	},
}

// GetCopyBuffer returns a pooled buffer for io.CopyBuffer operations.
func GetCopyBuffer() *[]byte {
	return copyBufferPool.Get().(*[]byte)
}

// PutCopyBuffer returns a buffer to the pool.
func PutCopyBuffer(buf *[]byte) {
	copyBufferPool.Put(buf)
}

// detectFormat reads magic bytes to determine compression format
// Returns "zstd", "gzip", or "raw"
func detectFormat(r io.ReadSeeker) (string, error) {
	header := make([]byte, 4)
	n, err := r.Read(header)
	if err != nil && err != io.EOF {
		return "", err
	}
	// Seek back to start
	_, seekErr := r.Seek(0, 0)
	if seekErr != nil {
		return "", seekErr
	}
	if n >= 4 && header[0] == zstdMagic0 && header[1] == zstdMagic1 &&
		header[2] == zstdMagic2 && header[3] == zstdMagic3 {
		return "zstd", nil
	}
	if n >= 2 && header[0] == gzipMagic0 && header[1] == gzipMagic1 {
		return "gzip", nil
	}
	return "raw", nil
}

func gzDelegateOpen(filename string, ungzip bool) (readable io.Reader, closer Closer, err error) {
	defer fail.Around(&err)

	source, err := os.Open(filename)
	fail.On(err != nil, "Failed to open %q -> %v", filename, err)

	if !ungzip {
		// No decompression requested
		return source, func() error { return source.Close() }, nil
	}

	// Detect format using magic bytes
	format, err := detectFormat(source)
	fail.On(err != nil, "Failed to detect format %q -> %v", filename, err)

	var reader io.Reader
	switch format {
	case "zstd":
		zr, cleanup, zErr := getPooledDecoder(source)
		if zErr != nil {
			source.Close()
			return nil, nil, zErr
		}
		reader = zr
		closer = func() error {
			cleanup() // Return decoder to pool
			return source.Close()
		}
	case "gzip":
		gr, gErr := gzip.NewReader(source)
		if gErr != nil {
			source.Close()
			return nil, nil, gErr
		}
		reader = gr
		closer = func() error {
			gr.Close()
			return source.Close()
		}
	default:
		// Raw file, no compression
		reader = source
		closer = func() error {
			return source.Close()
		}
	}
	return reader, closer, nil
}

func delegateOpen(it MutableLibrary, digest string, ungzip bool) (readable io.Reader, closer Closer, err error) {
	return gzDelegateOpen(it.ExactLocation(digest), ungzip)
}
