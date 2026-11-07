//go:build !linux && !darwin && !windows

package artifacttrust

func metadataStateLock(string) (func(), error) { return nil, metadataError(CodeMetadataState) }
func syncMetadataStateDirectory(string) error  { return metadataError(CodeMetadataState) }
