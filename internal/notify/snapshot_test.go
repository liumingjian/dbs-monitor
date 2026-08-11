package notify

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestChannelSnapshotStoreWritesEncryptedConfigurationAtomically(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "notification-channels.snapshot")
	store := NewChannelSnapshotStore(path)
	authVersion := int32(3)
	username := "monitor"
	want := ChannelSnapshot{
		FormatVersion: ChannelSnapshotFormatVersion,
		SMTP: &SnapshotSMTPChannel{
			Enabled: true, Host: "smtp.example.com", Port: 465,
			From: "monitor@example.com", Recipient: "dba@example.com",
			AuthType: "PLAIN", Username: &username,
			AuthCiphertext: []byte{0x01, 0x02, 0x03}, AuthKeyVersion: &authVersion,
			TLSMode: "IMPLICIT",
		},
		Webhooks: []SnapshotWebhookTarget{{
			ID: "7af340e7-70e8-468a-8129-d66a8ff4c968", Name: "on-call", Enabled: true,
			URL:                       "https://notify.example.com/platform",
			SigningValueCiphertext:    []byte{0x04, 0x05},
			SignatureHeaderCiphertext: []byte{0x06, 0x07},
			SigningKeyVersion:         3,
		}},
	}

	if err := store.Write(want); err != nil {
		t.Fatalf("write channel snapshot: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load channel snapshot: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded channel snapshot = %#v, want %#v", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat channel snapshot: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("channel snapshot mode = %o, want 600", info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		t.Fatalf("channel snapshot owner UID = %v, want %d", stat, os.Geteuid())
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read channel snapshot: %v", err)
	}
	for _, plaintext := range [][]byte{[]byte("smtp-plaintext-password"), []byte("webhook-plaintext-signing-value")} {
		if bytes.Contains(contents, plaintext) {
			t.Fatalf("channel snapshot contains plaintext secret %q", plaintext)
		}
	}

	want.SMTP.Host = "smtp-secondary.example.com"
	if err := store.Write(want); err != nil {
		t.Fatalf("replace channel snapshot: %v", err)
	}
	got, err = store.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("replaced channel snapshot = %#v, %v; want %#v", got, err, want)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read channel snapshot directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("channel snapshot directory entries = %v, want only final snapshot", entries)
	}
}
