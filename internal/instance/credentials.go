package instance

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
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
	smtpCredentialAAD                     = "smtp-channel:singleton:auth"
	webhookSigningValuePurpose            = "signing-value"
	webhookSignatureHeaderPurpose         = "signature-header"
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
	CredentialFaultKeyFormat         CredentialFaultCode = "CREDENTIAL_KEY_FORMAT_INVALID"
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
	case CredentialFaultKeyFormat:
		return "instance credential key must contain one line of standard base64"
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
	directory           string
	currentVersion      int32
	currentKey          []byte
	fault               error
	generatedInitialKey bool
}

func (keyring *CredentialKeyring) CurrentVersion() int32 {
	return keyring.currentVersion
}

func OpenCredentialKeyring(directory string, hasEncryptedCredentials bool) (*CredentialKeyring, error) {
	keyring := &CredentialKeyring{directory: directory}
	generatedInitialKey, err := initializeCredentialKeyring(directory, hasEncryptedCredentials)
	if err != nil {
		keyring.fault = err
		return keyring, err
	}
	keyring.generatedInitialKey = generatedInitialKey

	version, err := readCurrentCredentialKeyVersion(directory)
	if err != nil {
		keyring.fault = err
		return keyring, err
	}
	missingCode := CredentialFaultCurrentKey
	matches, globErr := filepath.Glob(filepath.Join(directory, credentialKeyFilenamePattern))
	if globErr == nil && len(matches) == 0 {
		missingCode = CredentialFaultMissingKey
	}
	key, err := readCredentialKey(directory, version, missingCode)
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

func (keyring *CredentialKeyring) GeneratedInitialKey() bool {
	return keyring.generatedInitialKey
}

func initializeCredentialKeyring(directory string, hasEncryptedCredentials bool) (bool, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return false, &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if !info.IsDir() {
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
	currentPath := filepath.Join(directory, credentialCurrentVersionFilename)
	if _, err := os.Lstat(currentPath); !errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	matches, globErr := filepath.Glob(filepath.Join(directory, credentialKeyFilenamePattern))
	if globErr != nil || len(matches) != 0 {
		return false, &CredentialFault{Code: CredentialFaultCurrentKey}
	}
	return generateInitialCredentialKeyring(directory)
}

func (keyring *CredentialKeyring) EncryptPassword(instanceID uuid.UUID, password string) ([]byte, int32, error) {
	return keyring.encrypt([]byte(password), credentialAAD(instanceID), "instance credential")
}

func (keyring *CredentialKeyring) EncryptSMTPPassword(password string) ([]byte, int32, error) {
	return keyring.encrypt([]byte(password), []byte(smtpCredentialAAD), "SMTP credential")
}

func (keyring *CredentialKeyring) EncryptWebhookSigningValue(targetID uuid.UUID, value string) ([]byte, int32, error) {
	return keyring.encrypt([]byte(value), webhookCredentialAAD(targetID, webhookSigningValuePurpose), "Webhook signing value")
}

func (keyring *CredentialKeyring) EncryptWebhookSignatureHeader(targetID uuid.UUID, header string) ([]byte, int32, error) {
	return keyring.encrypt([]byte(header), webhookCredentialAAD(targetID, webhookSignatureHeaderPurpose), "Webhook signature header")
}

func (keyring *CredentialKeyring) encrypt(plaintext, aad []byte, description string) ([]byte, int32, error) {
	if keyring.fault != nil {
		return nil, 0, keyring.fault
	}
	gcm, err := credentialGCM(keyring.currentKey)
	if err != nil {
		return nil, 0, err
	}
	nonce := make([]byte, credentialNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, 0, fmt.Errorf("generate %s nonce: %w", description, err)
	}
	envelope := make([]byte, credentialEnvelopeHeaderSize)
	envelope[0] = credentialEnvelopeVersion
	copy(envelope[1:], nonce)
	envelope = gcm.Seal(envelope, nonce, plaintext, aad)
	return envelope, keyring.currentVersion, nil
}

func (keyring *CredentialKeyring) DecryptPassword(instanceID uuid.UUID, envelope []byte, keyVersion int32) (string, error) {
	return keyring.decrypt(envelope, keyVersion, credentialAAD(instanceID))
}

func (keyring *CredentialKeyring) DecryptSMTPPassword(envelope []byte, keyVersion int32) (string, error) {
	return keyring.decrypt(envelope, keyVersion, []byte(smtpCredentialAAD))
}

func (keyring *CredentialKeyring) DecryptWebhookSigningValue(targetID uuid.UUID, envelope []byte, keyVersion int32) (string, error) {
	return keyring.decrypt(envelope, keyVersion, webhookCredentialAAD(targetID, webhookSigningValuePurpose))
}

func (keyring *CredentialKeyring) DecryptWebhookSignatureHeader(targetID uuid.UUID, envelope []byte, keyVersion int32) (string, error) {
	return keyring.decrypt(envelope, keyVersion, webhookCredentialAAD(targetID, webhookSignatureHeaderPurpose))
}

func (keyring *CredentialKeyring) decrypt(envelope []byte, keyVersion int32, aad []byte) (string, error) {
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
	plaintext, err := gcm.Open(nil, nonce, envelope[credentialEnvelopeHeaderSize:], aad)
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

func generateInitialCredentialKeyring(directory string) (bool, error) {
	if os.Geteuid() == 0 {
		return false, &CredentialFault{Code: CredentialFaultKeyOwner}
	}
	encodedKey, err := generateEncodedCredentialKey()
	if err != nil {
		return false, err
	}
	keyPath := filepath.Join(directory, credentialKeyFilename(1))
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, &CredentialFault{Code: CredentialFaultMissingKey}
	}
	keyFileWritten := false
	defer func() {
		file.Close()
		if !keyFileWritten {
			os.Remove(keyPath)
		}
	}()
	if bytesWritten, err := file.WriteString(encodedKey); err != nil || bytesWritten != len(encodedKey) {
		return false, &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if err := file.Sync(); err != nil {
		return false, &CredentialFault{Code: CredentialFaultMissingKey}
	}
	if err := file.Close(); err != nil {
		return false, &CredentialFault{Code: CredentialFaultMissingKey}
	}
	keyFileWritten = true
	if err := writeCurrentCredentialKeyVersion(directory, 1); err != nil {
		return false, err
	}
	return true, nil
}

func generateCredentialKey(directory string, version int32) error {
	encodedKey, err := generateEncodedCredentialKey()
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".master-key-")
	if err != nil {
		return &CredentialFault{Code: CredentialFaultMissingKey}
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if bytesWritten, err := file.WriteString(encodedKey); err != nil || bytesWritten != len(encodedKey) {
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

func generateEncodedCredentialKey() (string, error) {
	key := make([]byte, credentialKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("generate instance credential key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key) + "\n", nil
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
	contents, err := readCredentialFile(path, missingCode)
	if err != nil {
		return nil, err
	}
	line, ok := parseCredentialLine(contents)
	if !ok {
		return nil, &CredentialFault{Code: CredentialFaultKeyFormat}
	}
	key, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		return nil, &CredentialFault{Code: CredentialFaultKeyFormat}
	}
	if len(key) != credentialKeySize {
		return nil, &CredentialFault{Code: CredentialFaultKeyLength}
	}
	return key, nil
}

func readCredentialFile(path string, missingCode CredentialFaultCode) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, &CredentialFault{Code: missingCode}
	}
	if !info.Mode().IsRegular() {
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

func webhookCredentialAAD(targetID uuid.UUID, field string) []byte {
	return []byte("dbs-monitor:webhook:" + targetID.String() + ":" + field + ":v1")
}
