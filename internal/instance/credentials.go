package instance

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/google/uuid"
)

const (
	credentialEnvelopeVersion        byte = 1
	credentialKeySize                     = 32
	credentialNonceSize                   = 12
	credentialEnvelopeHeaderSize          = 1 + credentialNonceSize
	credentialCurrentVersionFilename      = "current"
	credentialKeyFilenamePattern          = "master-key-v*"
)

type CredentialFaultCode string

const (
	CredentialFaultMissingKey        CredentialFaultCode = "CREDENTIAL_KEY_MISSING"
	CredentialFaultCurrentKey        CredentialFaultCode = "CREDENTIAL_CURRENT_KEY_INVALID"
	CredentialFaultKeyLength         CredentialFaultCode = "CREDENTIAL_KEY_LENGTH_INVALID"
	CredentialFaultKeyPermissions    CredentialFaultCode = "CREDENTIAL_KEY_PERMISSIONS_INVALID"
	CredentialFaultKeyOwner          CredentialFaultCode = "CREDENTIAL_KEY_OWNER_INVALID"
	CredentialFaultUnknownKeyVersion CredentialFaultCode = "CREDENTIAL_KEY_VERSION_UNKNOWN"
	CredentialFaultAuthentication    CredentialFaultCode = "CREDENTIAL_AUTHENTICATION_FAILED"
)

// CredentialFault is the stable platform-fault fact consumed by diagnostics.
type CredentialFault struct {
	Code CredentialFaultCode
}

func (fault *CredentialFault) Error() string {
	switch fault.Code {
	case CredentialFaultMissingKey:
		return "instance credential key is missing"
	case CredentialFaultCurrentKey:
		return "instance credential current key is invalid"
	case CredentialFaultKeyLength:
		return "instance credential key length is invalid"
	case CredentialFaultKeyPermissions:
		return "instance credential key permissions are invalid"
	case CredentialFaultKeyOwner:
		return "instance credential key owner is invalid"
	case CredentialFaultUnknownKeyVersion:
		return "instance credential key version is unknown"
	case CredentialFaultAuthentication:
		return "instance credential authentication failed"
	default:
		return "instance credential failure"
	}
}

type CredentialVersion int64

func (version CredentialVersion) After(other CredentialVersion) bool {
	return version > other
}

// CredentialKeyring is the concrete instance-password implementation.
type CredentialKeyring struct {
	directory      string
	currentVersion int32
	currentKey     []byte
}

func OpenCredentialKeyring(directory string, hasEncryptedCredentials bool) (*CredentialKeyring, error) {
	if err := initializeCredentialKeyring(directory, hasEncryptedCredentials); err != nil {
		return nil, err
	}

	version, err := readCurrentCredentialKeyVersion(directory)
	if err != nil {
		return nil, err
	}
	missingCode := CredentialFaultCurrentKey
	if matches, globErr := filepath.Glob(filepath.Join(directory, credentialKeyFilenamePattern)); globErr == nil && len(matches) == 0 {
		missingCode = CredentialFaultMissingKey
	}
	key, err := readCredentialKey(directory, version, missingCode)
	if err != nil {
		return nil, err
	}
	return &CredentialKeyring{directory: directory, currentVersion: version, currentKey: key}, nil
}

func initializeCredentialKeyring(directory string, hasEncryptedCredentials bool) error {
	_, err := os.Stat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if hasEncryptedCredentials {
			return &CredentialFault{Code: CredentialFaultMissingKey}
		}
		return generateInitialCredentialKeyring(directory)
	}
	if err != nil {
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if hasEncryptedCredentials {
		return nil
	}
	if _, err := os.Stat(filepath.Join(directory, credentialCurrentVersionFilename)); !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	matches, globErr := filepath.Glob(filepath.Join(directory, credentialKeyFilenamePattern))
	if globErr != nil || len(matches) != 0 {
		return &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	return generateInitialCredentialKeyring(directory)
}

func (keyring *CredentialKeyring) EncryptPassword(instanceID uuid.UUID, password string) ([]byte, int32, error) {
	gcm, err := credentialGCM(keyring.currentKey)
	if err != nil {
		return nil, 0, err
	}
	nonce := make([]byte, credentialNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, 0, fmt.Errorf("generate instance credential nonce: %w", err)
	}
	envelope := make([]byte, credentialEnvelopeHeaderSize)
	envelope[0] = credentialEnvelopeVersion
	copy(envelope[1:], nonce)
	envelope = gcm.Seal(envelope, nonce, []byte(password), credentialAAD(instanceID))
	return envelope, keyring.currentVersion, nil
}

func (keyring *CredentialKeyring) DecryptPassword(instanceID uuid.UUID, envelope []byte, keyVersion int32) (string, error) {
	key := keyring.currentKey
	if keyVersion != keyring.currentVersion {
		var err error
		key, err = readCredentialKey(keyring.directory, keyVersion, CredentialFaultUnknownKeyVersion)
		if err != nil {
			return "", err
		}
	}
	gcm, err := credentialGCM(key)
	if err != nil {
		return "", err
	}
	if len(envelope) < credentialEnvelopeHeaderSize+gcm.Overhead() || envelope[0] != credentialEnvelopeVersion {
		return "", &CredentialFault{Code: CredentialFaultAuthentication}
	}
	nonce := envelope[1:credentialEnvelopeHeaderSize]
	plaintext, err := gcm.Open(nil, nonce, envelope[credentialEnvelopeHeaderSize:], credentialAAD(instanceID))
	if err != nil {
		return "", &CredentialFault{Code: CredentialFaultAuthentication}
	}
	return string(plaintext), nil
}

func generateInitialCredentialKeyring(directory string) error {
	if os.Geteuid() == 0 {
		return &CredentialFault{Code: CredentialFaultKeyOwner}
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	key := make([]byte, credentialKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return fmt.Errorf("generate instance credential key: %w", err)
	}
	keyPath := filepath.Join(directory, credentialKeyFilename(1))
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	written := false
	defer func() {
		file.Close()
		if !written {
			os.Remove(keyPath)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return &CredentialFault{Code: CredentialFaultKeyPermissions}
	}
	if _, err := file.Write(key); err != nil {
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if err := file.Sync(); err != nil {
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if err := file.Close(); err != nil {
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	written = true
	if err := writeCurrentCredentialKeyVersion(directory, 1); err != nil {
		return err
	}
	return nil
}

func readCurrentCredentialKeyVersion(directory string) (int32, error) {
	contents, err := os.ReadFile(filepath.Join(directory, credentialCurrentVersionFilename))
	if err != nil {
		return 0, &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(contents)), 10, 32)
	if err != nil || value <= 0 {
		return 0, &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	return int32(value), nil
}

func writeCurrentCredentialKeyVersion(directory string, version int32) error {
	file, err := os.CreateTemp(directory, ".current-")
	if err != nil {
		return &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	if _, err := fmt.Fprintf(file, "%d\n", version); err != nil {
		file.Close()
		return &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	if err := file.Close(); err != nil {
		return &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	if err := os.Rename(temporary, filepath.Join(directory, credentialCurrentVersionFilename)); err != nil {
		return &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	return nil
}

func readCredentialKey(directory string, version int32, missingCode CredentialFaultCode) ([]byte, error) {
	if version <= 0 {
		return nil, &CredentialFault{Code: missingCode}
	}
	path := filepath.Join(directory, credentialKeyFilename(version))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, &CredentialFault{Code: missingCode}
	}
	if info.Mode().Perm() != 0o600 {
		return nil, &CredentialFault{Code: CredentialFaultKeyPermissions}
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid == 0 || stat.Uid != uint32(os.Geteuid()) {
		return nil, &CredentialFault{Code: CredentialFaultKeyOwner}
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, &CredentialFault{Code: missingCode}
	}
	if len(key) != credentialKeySize {
		return nil, &CredentialFault{Code: CredentialFaultKeyLength}
	}
	return key, nil
}

func credentialKeyFilename(version int32) string {
	return fmt.Sprintf("master-key-v%d", version)
}

func credentialGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, &CredentialFault{Code: CredentialFaultKeyLength}
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, credentialNonceSize)
	if err != nil {
		return nil, &CredentialFault{Code: CredentialFaultAuthentication}
	}
	return gcm, nil
}

func credentialAAD(instanceID uuid.UUID) []byte {
	return []byte("dbs-monitor:instance:" + instanceID.String() + ":pg-password:v1")
}
