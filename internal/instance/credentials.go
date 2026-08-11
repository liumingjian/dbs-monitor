package instance

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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
	credentialKeyFilenamePrefix           = "master-key-v"
	credentialKeyFilenamePattern          = credentialKeyFilenamePrefix + "*"
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

func (keyring *CredentialKeyring) CurrentVersion() int32 {
	return keyring.currentVersion
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

// PrepareCredentialKeyRotation creates or resumes the filesystem side of an offline rotation.
// The boolean reports whether the transactional re-encryption phase must run before cleanup.
func PrepareCredentialKeyRotation(ctx context.Context, queries *Queries, directory string) (*CredentialKeyring, bool, error) {
	keyring, err := OpenCredentialKeyring(directory, true)
	if err != nil {
		return nil, false, err
	}
	versions, err := credentialKeyVersions(directory)
	if err != nil {
		return nil, false, err
	}
	credentialsNotUsingCurrentKey, err := queries.CountCredentialsNotUsingKeyVersion(ctx, keyring.currentVersion)
	if err != nil {
		return nil, false, fmt.Errorf("inspect instance credential key versions: %w", err)
	}

	nextVersion := keyring.currentVersion + 1
	if nextVersion <= keyring.currentVersion {
		return nil, false, fmt.Errorf("instance credential key version exhausted")
	}
	// Resume an attempt that staged the next key but did not update current.
	_, nextKeyExists := slices.BinarySearch(versions, nextVersion)
	if nextKeyExists {
		if _, err := readCredentialKey(directory, nextVersion, CredentialFaultCurrentKey); err != nil {
			return nil, false, err
		}
		keyring, err := activateCredentialKey(directory, nextVersion)
		return keyring, true, err
	}
	// Resume the row rewrite after current was updated by an earlier attempt.
	if credentialsNotUsingCurrentKey > 0 {
		return keyring, true, nil
	}
	// Old keys with no database references only need cleanup.
	for _, version := range versions {
		if version < keyring.currentVersion {
			return keyring, false, nil
		}
		if version > nextVersion {
			return nil, false, &CredentialFault{Code: CredentialFaultCurrentKey}
		}
	}

	if err := generateCredentialKey(directory, nextVersion); err != nil {
		return nil, false, err
	}
	keyring, err = activateCredentialKey(directory, nextVersion)
	return keyring, true, err
}

func activateCredentialKey(directory string, version int32) (*CredentialKeyring, error) {
	if err := writeCurrentCredentialKeyVersion(directory, version); err != nil {
		return nil, err
	}
	return OpenCredentialKeyring(directory, true)
}

func (keyring *CredentialKeyring) ReencryptCredentials(ctx context.Context, queries *Queries) (int64, error) {
	rows, err := queries.ListCredentialsForKeyRotation(ctx)
	if err != nil {
		return 0, fmt.Errorf("list instance credentials for key rotation: %w", err)
	}
	var rotated int64
	for _, row := range rows {
		if row.PasswordKeyVersion == keyring.currentVersion {
			continue
		}
		instanceID := uuid.UUID(row.ID.Bytes)
		password, err := keyring.DecryptPassword(instanceID, row.PasswordCiphertext, row.PasswordKeyVersion)
		if err != nil {
			return 0, err
		}
		ciphertext, version, err := keyring.EncryptPassword(instanceID, password)
		if err != nil {
			return 0, err
		}
		if err := queries.UpdateCredentialKeyVersion(ctx, UpdateCredentialKeyVersionParams{
			ID: row.ID, PasswordCiphertext: ciphertext, PasswordKeyVersion: version,
		}); err != nil {
			return 0, fmt.Errorf("update instance credential key version: %w", err)
		}
		rotated++
	}
	return rotated, nil
}

func (keyring *CredentialKeyring) RemoveUnreferencedKeys(ctx context.Context, queries *Queries) error {
	versions, err := credentialKeyVersions(keyring.directory)
	if err != nil {
		return err
	}
	// Verify every stale key before deleting any, so a referenced key leaves the keyring untouched.
	for _, version := range versions {
		if version == keyring.currentVersion {
			continue
		}
		references, err := queries.CountCredentialKeyReferences(ctx, version)
		if err != nil {
			return fmt.Errorf("verify instance credential key version %d references: %w", version, err)
		}
		if references != 0 {
			return fmt.Errorf("instance credential key version %d still has %d database references", version, references)
		}
	}
	for _, version := range versions {
		if version == keyring.currentVersion {
			continue
		}
		if err := os.Remove(filepath.Join(keyring.directory, credentialKeyFilename(version))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove instance credential key version %d: %w", version, err)
		}
	}
	return syncCredentialDirectory(keyring.directory, CredentialFaultMissingKey)
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

func generateCredentialKey(directory string, version int32) error {
	key := make([]byte, credentialKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return fmt.Errorf("generate instance credential key: %w", err)
	}
	file, err := os.CreateTemp(directory, ".master-key-")
	if err != nil {
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return &CredentialFault{Code: CredentialFaultKeyPermissions}
	}
	if _, err := file.Write(key); err != nil {
		file.Close()
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if err := file.Close(); err != nil {
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if err := os.Link(temporary, filepath.Join(directory, credentialKeyFilename(version))); err != nil {
		return &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	return syncCredentialDirectory(directory, CredentialFaultMissingKey)
}

func credentialKeyVersions(directory string) ([]int32, error) {
	matches, err := filepath.Glob(filepath.Join(directory, credentialKeyFilenamePattern))
	if err != nil {
		return nil, &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	versions := make([]int32, 0, len(matches))
	for _, match := range matches {
		value := strings.TrimPrefix(filepath.Base(match), credentialKeyFilenamePrefix)
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed <= 0 {
			return nil, &CredentialFault{Code: CredentialFaultCurrentKey}
		}
		version := int32(parsed)
		if _, err := readCredentialKey(directory, version, CredentialFaultCurrentKey); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	slices.Sort(versions)
	return versions, nil
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
	return syncCredentialDirectory(directory, CredentialFaultCurrentKey)
}

func syncCredentialDirectory(directory string, faultCode CredentialFaultCode) error {
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return &CredentialFault{Code: faultCode}
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return &CredentialFault{Code: faultCode}
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
	return fmt.Sprintf("%s%d", credentialKeyFilenamePrefix, version)
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
