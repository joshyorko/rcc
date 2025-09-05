package operations

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"

	"github.com/robocorp/rcc/common"
)

type Ephemeral interface {
	RequestBody(interface{}) (io.Reader, error)
	Decode([]byte) ([]byte, error)
}

type EncryptionKeys struct {
	Iv              string `json:"iv"`
	Atag            string `json:"atag"`
	EncryptedAESKey string `json:"encryptedAESKey"`
}

type EncryptionPayload struct {
	Encryption *EncryptionKeys `json:"encryption"`
	Payload    string          `json:"payload,omitempty"`
}

type EncryptionV1 struct {
	*rsa.PrivateKey
}

func Decoded(content string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(content)
}

func GenerateEphemeralKey() (Ephemeral, error) {
	common.Timeline("start ephemeral key generation")
	defer common.Timeline("done ephemeral key generation")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &EncryptionV1{key}, nil
}

func (it *EncryptionV1) PublicPEM() string {
	public, ok := it.Public().(*rsa.PublicKey)
	if !ok {
		return ""
	}
	block := &pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: []byte(x509.MarshalPKCS1PublicKey(public)),
	}
	result := pem.EncodeToMemory(block)
	return string(result)
}

func (it *EncryptionV1) RequestObject(payload interface{}) ([]byte, error) {
	result := make(Token)
	encryption := make(Token)
	encryption["scheme"] = "rc-encryption-v1"
	encryption["publicKey"] = it.PublicPEM()
	result["encryption"] = encryption
	if payload != nil {
		result["payload"] = payload
	}
	return json.Marshal(result)
}

func (it *EncryptionV1) RequestBody(payload interface{}) (io.Reader, error) {
	blob, err := it.RequestObject(payload)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(blob), nil
}

func (it *EncryptionV1) Decode(blob []byte) ([]byte, error) {
	var content EncryptionPayload
	err := json.Unmarshal(blob, &content)
	if err != nil {
		return nil, err
	}
	if content.Encryption == nil {
		return nil, errors.New("Reply from Cloud is not end-to-end encrypted (requirement)! Failing!")
	}
	ciphertext, err := Decoded(content.Encryption.EncryptedAESKey)
	if err != nil {
		return nil, err
	}
	secret, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, it.PrivateKey, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	iv, err := Decoded(content.Encryption.Iv)
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if aesgcm.NonceSize() != len(iv) {
		return nil, fmt.Errorf("Size difference in AES GCM nonce, %d vs. %d!", aesgcm.NonceSize(), len(iv))
	}
	atag, err := Decoded(content.Encryption.Atag)
	if err != nil {
		return nil, err
	}
	payload, err := Decoded(content.Payload)
	if err != nil {
		return nil, err
	}
	payload = append(payload, atag...)
	plaintext, err := aesgcm.Open(nil, iv, payload, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}
