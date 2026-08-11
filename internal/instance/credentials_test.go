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
	const (
		password   = "not-written-to-errors"
		gcmTagSize = 16
	)
	directory := filepath.Join(t.TempDir(), "credentials")
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

func TestSMTPPasswordUsesDistinctAuthenticatedPurpose(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	keyring, err := OpenCredentialKeyring(directory, false)
	if err != nil {
		t.Fatalf("open new keyring: %v", err)
	}
	ciphertext, version, err := keyring.EncryptSMTPPassword("smtp-auth-value")
	if err != nil {
		t.Fatalf("encrypt SMTP password: %v", err)
	}
	if got, err := keyring.DecryptSMTPPassword(ciphertext, version); err != nil || got != "smtp-auth-value" {
		t.Fatalf("decrypt SMTP password = %q, %v", got, err)
	}
	if _, err := keyring.DecryptPassword(uuid.New(), ciphertext, version); err == nil {
		t.Fatal("SMTP ciphertext decrypted as an instance password")
	}
}

func TestWebhookSigningFieldsUseTargetBoundAuthenticatedPurposes(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	keyring, err := OpenCredentialKeyring(directory, false)
	if err != nil {
		t.Fatalf("open new keyring: %v", err)
	}
	targetID := uuid.MustParse("00000000-0000-0000-0000-000000000080")
	valueCiphertext, version, err := keyring.EncryptWebhookSigningValue(targetID, "signing-value")
	if err != nil {
		t.Fatalf("encrypt Webhook signing value: %v", err)
	}
	headerCiphertext, headerVersion, err := keyring.EncryptWebhookSignatureHeader(targetID, "X-DBS-Signature")
	if err != nil {
		t.Fatalf("encrypt Webhook signature header: %v", err)
	}
	if got, err := keyring.DecryptWebhookSigningValue(targetID, valueCiphertext, version); err != nil || got != "signing-value" {
		t.Fatalf("decrypt Webhook signing value = %q, %v", got, err)
	}
	if got, err := keyring.DecryptWebhookSignatureHeader(targetID, headerCiphertext, headerVersion); err != nil || got != "X-DBS-Signature" {
		t.Fatalf("decrypt Webhook signature header = %q, %v", got, err)
	}
	if _, err := keyring.DecryptWebhookSigningValue(uuid.New(), valueCiphertext, version); err == nil {
		t.Fatal("Webhook ciphertext decrypted for another target")
	}
	if _, err := keyring.DecryptWebhookSigningValue(targetID, headerCiphertext, headerVersion); err == nil {
		t.Fatal("Webhook signature header decrypted as a signing value")
	}
}

func TestCredentialKeyringGeneratesOnlyOnce(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("create empty credential directory: %v", err)
	}
	if _, err := OpenCredentialKeyring(directory, false); err != nil {
		t.Fatalf("open new keyring: %v", err)
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

func TestCredentialKeyringRejectsOrphanedKey(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	writeCredentialFile(t, directory, credentialKeyFilename(1), make([]byte, credentialKeySize), 0o600)

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
				writeCredentialFile(t, directory, credentialKeyFilename(1), make([]byte, credentialKeySize), 0o600)
			},
			want: CredentialFaultCurrentKey,
		},
		{
			name: "wrong key length",
			setup: func(t *testing.T, directory string) {
				writeCredentialFile(t, directory, credentialCurrentVersionFilename, []byte("1\n"), 0o600)
				writeCredentialFile(t, directory, credentialKeyFilename(1), make([]byte, credentialKeySize-1), 0o600)
			},
			want: CredentialFaultKeyLength,
		},
		{
			name: "wrong key permissions",
			setup: func(t *testing.T, directory string) {
				writeCredentialFile(t, directory, credentialCurrentVersionFilename, []byte("1\n"), 0o600)
				writeCredentialFile(t, directory, credentialKeyFilename(1), make([]byte, credentialKeySize), 0o640)
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
	keyring, err := OpenCredentialKeyring(filepath.Join(t.TempDir(), "credentials"), false)
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

func writeCredentialFile(t *testing.T, directory, name string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("create credential directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), contents, mode); err != nil {
		t.Fatalf("write credential fixture: %v", err)
	}
}
