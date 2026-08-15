package instance

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCredentialKeyringRoundTripAndNonceIsolation(t *testing.T) {
	const (
		password   = "not-written-to-errors"
		gcmTagSize = 16
	)
	directory := createCredentialDirectory(t)
	keyring, err := OpenCredentialKeyring(directory, false)
	if err != nil {
		t.Fatalf("open new keyring: %v", err)
	}
	instanceID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	first, firstVersion, err := keyring.EncryptPassword(instanceID, password)
	if err != nil {
		t.Fatalf("encrypt first password: %v", err)
	}
	second, secondVersion, err := keyring.EncryptPassword(instanceID, password)
	if err != nil {
		t.Fatalf("encrypt second password: %v", err)
	}
	if firstVersion != secondVersion || firstVersion != 1 {
		t.Fatalf("key versions = %d, %d, want 1, 1", firstVersion, secondVersion)
	}
	if len(first) != credentialEnvelopeHeaderSize+len(password)+gcmTagSize {
		t.Fatalf("envelope length = %d, want format byte + nonce + ciphertext/tag", len(first))
	}
	if bytes.Equal(first, second) {
		t.Fatal("two encryptions reused an envelope")
	}
	if bytes.Equal(first[1:credentialEnvelopeHeaderSize], second[1:credentialEnvelopeHeaderSize]) {
		t.Fatal("two encryptions reused a nonce")
	}

	plaintext, err := keyring.DecryptPassword(instanceID, first, firstVersion)
	if err != nil {
		t.Fatalf("decrypt password: %v", err)
	}
	if plaintext != password {
		t.Fatal("decrypted password did not round-trip")
	}

	tampered := append([]byte(nil), first...)
	tampered[len(tampered)-1] ^= 1
	assertCredentialFault(t, decryptError(keyring, instanceID, tampered, firstVersion), CredentialFaultAuthentication)
	otherInstance := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	err = decryptError(keyring, otherInstance, first, firstVersion)
	assertCredentialFault(t, err, CredentialFaultAuthentication)
	if strings.Contains(err.Error(), password) {
		t.Fatalf("credential fault exposed plaintext: %v", err)
	}
}

func TestCredentialKeyringGeneratesOnlyOnce(t *testing.T) {
	directory := createCredentialDirectory(t)
	keyring, err := OpenCredentialKeyring(directory, false)
	if err != nil {
		t.Fatalf("open new keyring: %v", err)
	}
	if !keyring.Generated() {
		t.Fatal("new keyring was not reported as generated")
	}
	if keyring.CurrentVersion() != 1 {
		t.Fatalf("current key version = %d, want 1", keyring.CurrentVersion())
	}
	path := filepath.Join(directory, credentialKeyFilename(1))
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated key: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat generated key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key permissions = %04o, want 0600", info.Mode().Perm())
	}
	current, err := os.ReadFile(filepath.Join(directory, credentialCurrentVersionFilename))
	if err != nil {
		t.Fatalf("read current key pointer: %v", err)
	}
	if string(current) != "1\n" {
		t.Fatalf("current key pointer = %q, want version 1", current)
	}
	keyring, err = OpenCredentialKeyring(directory, false)
	if err != nil {
		t.Fatalf("reopen keyring: %v", err)
	}
	if keyring.Generated() {
		t.Fatal("existing keyring was reported as generated")
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reread generated key: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("reopening replaced the master key")
	}
}

func TestCredentialKeyringRequiresPrecreatedDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")

	_, err := OpenCredentialKeyring(directory, false)
	assertCredentialFault(t, err, CredentialFaultMissingKey)
	if _, statErr := os.Stat(directory); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("credential directory stat error = %v, want not exist", statErr)
	}
}

func TestCredentialKeyringStoresSingleLineBase64(t *testing.T) {
	directory := createCredentialDirectory(t)
	if _, err := OpenCredentialKeyring(directory, false); err != nil {
		t.Fatalf("open new keyring: %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(directory, credentialKeyFilename(1)))
	if err != nil {
		t.Fatalf("read generated key: %v", err)
	}
	encodedKey := strings.TrimSuffix(string(contents), "\n")
	if encodedKey == string(contents) || strings.ContainsAny(encodedKey, "\r\n") {
		t.Fatalf("master key is not one newline-terminated line")
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		t.Fatalf("decode generated master key: %v", err)
	}
	if len(key) != credentialKeySize {
		t.Fatalf("decoded master key length = %d, want %d", len(key), credentialKeySize)
	}
}

func TestCredentialKeyringRetainsOpenFault(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	writeCredentialFile(t, directory, credentialCurrentVersionFilename, []byte("1\n"), 0o600)
	writeCredentialFile(t, directory, credentialKeyFilename(1), credentialKeyFixture(credentialKeySize), 0o640)

	keyring, err := OpenCredentialKeyring(directory, true)
	assertCredentialFault(t, err, CredentialFaultKeyPermissions)
	if keyring == nil {
		t.Fatal("open failure returned no keyring for runtime fault propagation")
	}
	assertCredentialFault(t, keyring.Fault(), CredentialFaultKeyPermissions)
	_, _, err = keyring.EncryptPassword(uuid.New(), "must-not-leak")
	assertCredentialFault(t, err, CredentialFaultKeyPermissions)
}

func TestCredentialKeyringRejectsOrphanedKey(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	writeCredentialFile(t, directory, credentialKeyFilename(1), credentialKeyFixture(credentialKeySize), 0o600)

	_, err := OpenCredentialKeyring(directory, false)
	assertCredentialFault(t, err, CredentialFaultCurrentKey)
}

func TestCredentialKeyringFaults(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
		want  CredentialFaultCode
	}{
		{
			name: "missing keyring with encrypted credentials",
			want: CredentialFaultMissingKey,
		},
		{
			name: "wrong directory permissions",
			setup: func(t *testing.T, directory string) {
				if err := os.MkdirAll(directory, 0o700); err != nil {
					t.Fatalf("create credential directory: %v", err)
				}
				if err := os.Chmod(directory, 0o750); err != nil {
					t.Fatalf("change credential directory permissions: %v", err)
				}
			},
			want: CredentialFaultKeyPermissions,
		},
		{
			name: "missing current master key",
			setup: func(t *testing.T, directory string) {
				writeCredentialFile(t, directory, credentialCurrentVersionFilename, []byte("1\n"), 0o600)
			},
			want: CredentialFaultMissingKey,
		},
		{
			name: "dangling current",
			setup: func(t *testing.T, directory string) {
				writeCredentialFile(t, directory, credentialCurrentVersionFilename, []byte("2\n"), 0o600)
				writeCredentialFile(t, directory, credentialKeyFilename(1), credentialKeyFixture(credentialKeySize), 0o600)
			},
			want: CredentialFaultCurrentKey,
		},
		{
			name: "wrong key length",
			setup: func(t *testing.T, directory string) {
				writeCredentialFile(t, directory, credentialCurrentVersionFilename, []byte("1\n"), 0o600)
				writeCredentialFile(t, directory, credentialKeyFilename(1), credentialKeyFixture(credentialKeySize-1), 0o600)
			},
			want: CredentialFaultKeyLength,
		},
		{
			name: "wrong key permissions",
			setup: func(t *testing.T, directory string) {
				writeCredentialFile(t, directory, credentialCurrentVersionFilename, []byte("1\n"), 0o600)
				writeCredentialFile(t, directory, credentialKeyFilename(1), credentialKeyFixture(credentialKeySize), 0o640)
			},
			want: CredentialFaultKeyPermissions,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "credentials")
			if test.setup != nil {
				test.setup(t, directory)
			}
			_, err := OpenCredentialKeyring(directory, true)
			assertCredentialFault(t, err, test.want)
		})
	}
}

func TestCredentialKeyringRejectsUnknownVersion(t *testing.T) {
	directory := createCredentialDirectory(t)
	keyring, err := OpenCredentialKeyring(directory, false)
	if err != nil {
		t.Fatalf("open keyring: %v", err)
	}
	err = decryptError(keyring, uuid.New(), nil, 2)
	assertCredentialFault(t, err, CredentialFaultUnknownKeyVersion)
}

func TestCredentialVersionIsMonotonic(t *testing.T) {
	if !CredentialVersion(2).After(CredentialVersion(1)) {
		t.Fatal("newer credential version is not ordered after the old version")
	}
	if CredentialVersion(1).After(CredentialVersion(1)) {
		t.Fatal("equal credential versions compare as newer")
	}
}

func decryptError(keyring *CredentialKeyring, id uuid.UUID, ciphertext []byte, version int32) error {
	_, err := keyring.DecryptPassword(id, ciphertext, version)
	return err
}

func assertCredentialFault(t *testing.T, err error, want CredentialFaultCode) {
	t.Helper()
	var fault *CredentialFault
	if !errors.As(err, &fault) {
		t.Fatalf("error = %v, want CredentialFault", err)
	}
	if fault.Code != want {
		t.Fatalf("fault code = %q, want %q", fault.Code, want)
	}
}

func createCredentialDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "credentials")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create credential directory: %v", err)
	}
	return directory
}

func credentialKeyFixture(size int) []byte {
	return []byte(base64.StdEncoding.EncodeToString(make([]byte, size)) + "\n")
}

func writeCredentialFile(t *testing.T, directory, name string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("create credential directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), contents, mode); err != nil {
		t.Fatalf("write credential fixture: %v", err)
	}
}
