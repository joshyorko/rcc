package artifacttrust

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ReceiptStore keeps a latest receipt for compatibility and an append-only
// history for audit. Each record is canonical JSON and is written owner-only.
type ReceiptStore struct{ Root string }

var receiptLocks sync.Map

func NewReceiptStore(root string) *ReceiptStore { return &ReceiptStore{Root: root} }

func receiptName(artifact string) string {
	return strings.ReplaceAll(artifact, ":", "_")
}

func (s *ReceiptStore) Put(receipt VerificationReceipt) error {
	if receipt.ArtifactDigest == "" {
		return fmt.Errorf("receipt artifact digest is required")
	}
	if strings.ContainsAny(receipt.ArtifactDigest, `/\\`) {
		return fmt.Errorf("receipt artifact digest is unsafe")
	}
	for _, value := range []string{receipt.Diagnostic, receipt.PolicyRevision, receipt.Platform, receipt.Builder, receipt.RevocationSource} {
		if containsCredential(value) {
			return fmt.Errorf("receipt contains disallowed credential data")
		}
	}
	if receipt.DecisionID == "" {
		receipt = receipt.withDecisionID()
	}
	lockKey := filepath.Join(s.Root, receiptName(receipt.ArtifactDigest))
	lockValue, _ := receiptLocks.LoadOrStore(lockKey, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	data, err := receipt.JSON()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		return err
	}
	latest := filepath.Join(s.Root, receiptName(receipt.ArtifactDigest)+".json")
	if err := writeReceiptAtomically(latest, data); err != nil {
		return err
	}
	history := filepath.Join(s.Root, receiptName(receipt.ArtifactDigest)+".history.jsonl")
	if info, err := os.Lstat(history); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("receipt history target is a symlink")
	}
	file, err := os.OpenFile(history, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (s *ReceiptStore) History(artifact string) ([]VerificationReceipt, error) {
	if strings.ContainsAny(artifact, `/\\`) {
		return nil, fmt.Errorf("receipt artifact digest is unsafe")
	}
	file, err := os.Open(filepath.Join(s.Root, receiptName(artifact)+".history.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return []VerificationReceipt{}, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var result []VerificationReceipt
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), maxCarrierAttachmentBytes)
	for scanner.Scan() {
		var receipt VerificationReceipt
		if err := decodeStrict(scanner.Bytes(), &receipt); err != nil {
			return nil, fmt.Errorf("decode receipt history: %w", err)
		}
		result = append(result, receipt)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func writeReceiptAtomically(filePath string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(filePath), ".receipt-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()
	if err := temporary.Chmod(0600); err != nil {
		return err
	}
	if _, err := io.Copy(temporary, bytes.NewReader(data)); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, filePath)
}
