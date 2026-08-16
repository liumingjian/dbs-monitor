package notify

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
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

func TestChannelSnapshotStoreStopsWritesAtLocalDiskEmergency(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "notification-channels.snapshot")
	store := NewChannelSnapshotStore(path)
	store.SetLocalWriteAllowed(func() bool { return false })
	err := store.Write(ChannelSnapshot{FormatVersion: ChannelSnapshotFormatVersion})
	if !errors.Is(err, ErrLocalLargeWriteRejected) {
		t.Fatalf("snapshot write error = %v, want local large write rejection", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot was written at local disk emergency: %v", err)
	}
}

func TestChannelSnapshotStoreListsAndDeletesSnapshotWithEvent(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	name := "notification-channels.snapshot"
	path := filepath.Join(directory, name)
	store := NewChannelSnapshotStore(path)
	if err := store.Write(ChannelSnapshot{
		FormatVersion: ChannelSnapshotFormatVersion,
		Webhooks:      []SnapshotWebhookTarget{},
	}); err != nil {
		t.Fatalf("write channel snapshot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "unmanaged.snapshot"), []byte("keep"), 0o600); err != nil {
		t.Fatalf("write unmanaged file: %v", err)
	}
	modifiedAt := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, modifiedAt, modifiedAt); err != nil {
		t.Fatalf("set channel snapshot time: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat channel snapshot: %v", err)
	}

	gotArtifacts, err := store.ListArtifacts()
	if err != nil {
		t.Fatalf("list channel snapshot artifacts: %v", err)
	}
	wantArtifacts := []ChannelSnapshotArtifact{{
		Name:       name,
		Bytes:      info.Size(),
		ModifiedAt: modifiedAt,
	}}
	if !reflect.DeepEqual(gotArtifacts, wantArtifacts) {
		t.Fatalf("listed channel snapshot artifacts = %+v, want %+v", gotArtifacts, wantArtifacts)
	}

	var gotEvent ChannelSnapshotDeletionEvent
	deletedAt := modifiedAt.Add(time.Hour)
	if err := store.DeleteArtifact(name, deletedAt, func(candidate ChannelSnapshotDeletionEvent) error {
		gotEvent = candidate
		return nil
	}); err != nil {
		t.Fatalf("delete channel snapshot artifact: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted channel snapshot still exists: %v", err)
	}
	wantEvent := ChannelSnapshotDeletionEvent{
		Event:        "notification_channel_snapshot_deleted",
		SnapshotName: name,
		Bytes:        info.Size(),
		DeletedAt:    deletedAt,
	}
	if !reflect.DeepEqual(gotEvent, wantEvent) {
		t.Fatalf("channel snapshot deletion event = %+v, want %+v", gotEvent, wantEvent)
	}
	if _, err := os.Stat(filepath.Join(directory, "unmanaged.snapshot")); err != nil {
		t.Fatalf("delete removed unmanaged file: %v", err)
	}
}

func TestChannelSnapshotStoreRestoresSnapshotWhenDeletionEventFails(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	name := "notification-channels.snapshot"
	path := filepath.Join(directory, name)
	store := NewChannelSnapshotStore(path)
	if err := store.Write(ChannelSnapshot{
		FormatVersion: ChannelSnapshotFormatVersion,
		Webhooks:      []SnapshotWebhookTarget{},
	}); err != nil {
		t.Fatalf("write channel snapshot: %v", err)
	}

	recorderError := errors.New("platform event unavailable")
	err := store.DeleteArtifact(name, time.Now(), func(ChannelSnapshotDeletionEvent) error {
		return recorderError
	})
	if !errors.Is(err, recorderError) {
		t.Fatalf("delete channel snapshot error = %v, want event error", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("channel snapshot was not restored after event failure: %v", err)
	}
}

func TestChannelSnapshotStoreRejectsInvalidDeletionRequests(t *testing.T) {
	t.Parallel()

	name := "notification-channels.snapshot"
	store := NewChannelSnapshotStore(filepath.Join(t.TempDir(), name))
	if err := store.DeleteArtifact("../"+name, time.Time{}, func(ChannelSnapshotDeletionEvent) error {
		return nil
	}); err == nil {
		t.Fatal("delete channel snapshot accepted path traversal")
	}
	if err := store.DeleteArtifact(name, time.Time{}, nil); err == nil {
		t.Fatal("delete channel snapshot accepted a missing event recorder")
	}
}
