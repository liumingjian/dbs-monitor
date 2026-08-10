package instance

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCredentialKeyringRoundTripAndNonceIsolation(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	keyring, err := OpenCredentialKeyring(directory, false)
	if err != nil {
		t.Fatalf("open new keyring: %v", err)
	}
	instanceID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	first, firstVersion, err := keyring.EncryptPassword(instanceID, "not-written-to-errors")
	if err != nil {
		t.Fatalf("encrypt first password: %v", err)
	}
	second, secondVersion, err := keyring.EncryptPassword(instanceID, "not-written-to-errors")
	if err != nil {
		t.Fatalf("encrypt second password: %v", err)
	}
	if firstVersion != secondVersion || firstVersion != 1 {
		t.Fatalf("key versions = %d, %d, want 1, 1", firstVersion, secondVersion)
	}
	if len(first) != 1+12+len("not-written-to-errors")+16 {
		t.Fatalf("envelope length = %d, want format byte + nonce + ciphertext/tag", len(first))
	}
	if bytes.Equal(first, second) {
		t.Fatal("two encryptions reused an envelope")
	}
	if len(first) < 13 || len(second) < 13 || bytes.Equal(first[1:13], second[1:13]) {
		t.Fatal("two encryptions reused a nonce")
	}

	plaintext, err := keyring.DecryptPassword(instanceID, first, firstVersion)
	if err != nil {
		t.Fatalf("decrypt password: %v", err)
	}
	if plaintext != "not-written-to-errors" {
		t.Fatal("decrypted password did not round-trip")
	}

	tampered := append([]byte(nil), first...)
	tampered[len(tampered)-1] ^= 1
	assertCredentialFault(t, decryptError(keyring, instanceID, tampered, firstVersion), CredentialFaultAuthentication)
	otherInstance := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	if err := decryptError(keyring, otherInstance, first, firstVersion); err == nil {
		t.Fatal("ciphertext copied to another instance decrypted successfully")
	} else {
		assertCredentialFault(t, err, CredentialFaultAuthentication)
		if strings.Contains(err.Error(), "not-written-to-errors") {
			t.Fatalf("credential fault exposed plaintext: %v", err)
		}
	}
}

func TestCredentialKeyringGeneratesOnlyOnce(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	if _, err := OpenCredentialKeyring(directory, false); err != nil {
		t.Fatalf("open new keyring: %v", err)
	}
	path := filepath.Join(directory, "master-key-v1")
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
	current, err := os.ReadFile(filepath.Join(directory, "current"))
	if err != nil {
		t.Fatalf("read current key pointer: %v", err)
	}
	if string(current) != "1\n" {
		t.Fatalf("current key pointer = %q, want version 1", current)
	}
	if _, err := OpenCredentialKeyring(directory, false); err != nil {
		t.Fatalf("reopen keyring: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reread generated key: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("reopening replaced the master key")
	}
}

func TestCredentialKeyringFaults(t *testing.T) {
	tests := []struct {
		name string
		make func(*testing.T, string)
		want CredentialFaultCode
	}{
		{
			name: "missing keyring with encrypted credentials",
			want: CredentialFaultMissingKey,
		},
		{
			name: "missing current master key",
			make: func(t *testing.T, directory string) {
				writeCredentialFile(t, directory, "current", []byte("1\n"), 0o600)
			},
			want: CredentialFaultMissingKey,
		},
		{
			name: "dangling current",
			make: func(t *testing.T, directory string) {
				writeCredentialFile(t, directory, "current", []byte("2\n"), 0o600)
				writeCredentialFile(t, directory, "master-key-v1", make([]byte, 32), 0o600)
			},
			want: CredentialFaultCurrentKey,
		},
		{
			name: "wrong key length",
			make: func(t *testing.T, directory string) {
				writeCredentialFile(t, directory, "current", []byte("1\n"), 0o600)
				writeCredentialFile(t, directory, "master-key-v1", make([]byte, 31), 0o600)
			},
			want: CredentialFaultKeyLength,
		},
		{
			name: "wrong key permissions",
			make: func(t *testing.T, directory string) {
				writeCredentialFile(t, directory, "current", []byte("1\n"), 0o600)
				writeCredentialFile(t, directory, "master-key-v1", make([]byte, 32), 0o640)
			},
			want: CredentialFaultKeyPermissions,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "credentials")
			if test.make != nil {
				test.make(t, directory)
			}
			_, err := OpenCredentialKeyring(directory, true)
			assertCredentialFault(t, err, test.want)
		})
	}
}

func TestCredentialKeyringRejectsUnknownVersion(t *testing.T) {
	keyring, err := OpenCredentialKeyring(filepath.Join(t.TempDir(), "credentials"), false)
	if err != nil {
		t.Fatalf("open keyring: %v", err)
	}
	err = decryptError(keyring, uuid.New(), make([]byte, 29), 2)
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

func writeCredentialFile(t *testing.T, directory, name string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("create credential directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), contents, mode); err != nil {
		t.Fatalf("write credential fixture: %v", err)
	}
}
