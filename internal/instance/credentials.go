package instance

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
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
	fault          error
	generated      bool
}

func OpenCredentialKeyring(directory string, hasEncryptedCredentials bool) (*CredentialKeyring, error) {
	keyring := &CredentialKeyring{directory: directory}
	generated, err := initializeCredentialKeyring(directory, hasEncryptedCredentials)
	if err != nil {
		keyring.fault = err
		return keyring, err
	}
	keyring.generated = generated

	version, err := readCurrentCredentialKeyVersion(directory)
	if err != nil {
		keyring.fault = err
		return keyring, err
	}
	matches, globErr := filepath.Glob(filepath.Join(directory, credentialKeyFilenamePattern))
	missingKeyCode := CredentialFaultCurrentKey
	if globErr == nil && len(matches) == 0 {
		missingKeyCode = CredentialFaultMissingKey
	}
	key, err := readCredentialKey(directory, version, missingKeyCode)
	if err != nil {
		keyring.fault = err
		return keyring, err
	}
	keyring.currentVersion = version
	keyring.currentKey = key
	return keyring, nil
}

func (keyring *CredentialKeyring) Fault() error {
	return keyring.fault
}

func (keyring *CredentialKeyring) Generated() bool {
	return keyring.generated
}

func (keyring *CredentialKeyring) CurrentVersion() int32 {
	return keyring.currentVersion
}

func initializeCredentialKeyring(directory string, hasEncryptedCredentials bool) (bool, error) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() {
		return false, &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if info.Mode().Perm() != 0o700 {
		return false, &CredentialFault{Code: CredentialFaultKeyPermissions}
	}
	if !credentialOwnerIsValid(info) {
		return false, &CredentialFault{Code: CredentialFaultKeyOwner}
	}
	if hasEncryptedCredentials {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(directory, credentialCurrentVersionFilename)); !errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	matches, globErr := filepath.Glob(filepath.Join(directory, credentialKeyFilenamePattern))
	if globErr != nil || len(matches) != 0 {
		return false, &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	return generateInitialCredentialKeyring(directory)
}

func (keyring *CredentialKeyring) EncryptPassword(instanceID uuid.UUID, password string) ([]byte, int32, error) {
	if keyring.fault != nil {
		return nil, 0, keyring.fault
	}
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
	if keyring.fault != nil {
		return "", keyring.fault
	}
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

func generateInitialCredentialKeyring(directory string) (bool, error) {
	if os.Geteuid() == 0 {
		return false, &CredentialFault{Code: CredentialFaultKeyOwner}
	}
	key := make([]byte, credentialKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return false, fmt.Errorf("generate instance credential key: %w", err)
	}
	keyPath := filepath.Join(directory, credentialKeyFilename(1))
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, &CredentialFault{Code: CredentialFaultMissingKey}
	}
	keyPersisted := false
	defer func() {
		file.Close()
		if !keyPersisted {
			os.Remove(keyPath)
		}
	}()
	encodedKey := base64.StdEncoding.EncodeToString(key) + "\n"
	if bytesWritten, err := file.WriteString(encodedKey); err != nil || bytesWritten != len(encodedKey) {
		return false, &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if err := file.Sync(); err != nil {
		return false, &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if err := file.Close(); err != nil {
		return false, &CredentialFault{Code: CredentialFaultMissingKey}
	}
	keyPersisted = true
	if err := writeCurrentCredentialKeyVersion(directory, 1); err != nil {
		return false, err
	}
	return true, nil
}

func readCurrentCredentialKeyVersion(directory string) (int32, error) {
	contents, err := readCredentialFile(filepath.Join(directory, credentialCurrentVersionFilename), CredentialFaultCurrentKey)
	if err != nil {
		return 0, err
	}
	line, ok := parseCredentialLine(contents)
	if !ok {
		return 0, &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	value, err := strconv.ParseInt(line, 10, 32)
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
	contents, err := readCredentialFile(path, missingCode)
	if err != nil {
		return nil, err
	}
	line, ok := parseCredentialLine(contents)
	if !ok {
		return nil, &CredentialFault{Code: CredentialFaultKeyLength}
	}
	key, err := base64.StdEncoding.DecodeString(line)
	if err != nil || len(key) != credentialKeySize {
		return nil, &CredentialFault{Code: CredentialFaultKeyLength}
	}
	return key, nil
}

func readCredentialFile(path string, missingCode CredentialFaultCode) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, &CredentialFault{Code: missingCode}
	}
	if info.Mode().Perm() != 0o600 {
		return nil, &CredentialFault{Code: CredentialFaultKeyPermissions}
	}
	if !credentialOwnerIsValid(info) {
		return nil, &CredentialFault{Code: CredentialFaultKeyOwner}
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, &CredentialFault{Code: missingCode}
	}
	return contents, nil
}

func parseCredentialLine(contents []byte) (string, bool) {
	line := strings.TrimSuffix(string(contents), "\n")
	return line, line != "" && !strings.ContainsAny(line, "\r\n")
}

func credentialOwnerIsValid(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid != 0 && stat.Uid == uint32(os.Geteuid())
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
