package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log"
	"syscall"
	"unsafe"
)

const credTarget = "Noosphere/GatewayKey"

// ─── AES-256-GCM ───

// Encrypt encrypts plaintext with AES-256-GCM using the given 32-byte key.
// Returns base64-encoded ciphertext (nonce + encrypted data + tag).
func Encrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded ciphertext using the given 32-byte key.
func Decrypt(key []byte, encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

// GenerateKey creates a random 32-byte AES-256 key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// ─── Windows Credential Manager ───

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

// StoreMasterKey saves a key to Windows Credential Manager.
func StoreMasterKey(key []byte) error {
	target, _ := syscall.UTF16PtrFromString(credTarget)
	blob := key

	c := credential{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalMachine,
	}

	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	credWrite := advapi32.NewProc("CredWriteW")
	r, _, _ := credWrite.Call(
		uintptr(unsafe.Pointer(&c)),
		0,
	)
	if r == 0 {
		return fmt.Errorf("CredWriteW failed")
	}
	log.Printf("[secure] master key stored in Credential Manager")
	return nil
}

// LoadMasterKey retrieves a key from Windows Credential Manager.
func LoadMasterKey() ([]byte, error) {
	target, _ := syscall.UTF16PtrFromString(credTarget)
	var pcred *credential

	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	credRead := advapi32.NewProc("CredReadW")
	r, _, _ := credRead.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(credTypeGeneric),
		0,
		uintptr(unsafe.Pointer(&pcred)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CredReadW failed: key not found")
	}

	c := *pcred
	key := make([]byte, c.CredentialBlobSize)
	// Copy blob data
	for i := uint32(0); i < c.CredentialBlobSize; i++ {
		key[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(c.CredentialBlob)) + uintptr(i)))
	}

	// Free credential
	credFree := advapi32.NewProc("CredFree")
	credFree.Call(uintptr(unsafe.Pointer(pcred)))

	return key, nil
}

// GetOrCreateKey loads the master key from Credential Manager or creates a new one.
func GetOrCreateKey() ([]byte, error) {
	key, err := LoadMasterKey()
	if err == nil {
		return key, nil
	}
	log.Printf("[secure] no existing key, generating new AES-256 key")
	key, err = GenerateKey()
	if err != nil {
		return nil, err
	}
	if err := StoreMasterKey(key); err != nil {
		return nil, fmt.Errorf("persist credential encryption key: %w", err)
	}
	return key, nil
}

// Btoi converts big-endian bytes to uint32 (for compatibility).
func Btoi(b []byte) uint32 {
	return binary.BigEndian.Uint32(b)
}
