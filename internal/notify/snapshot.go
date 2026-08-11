package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const ChannelSnapshotFormatVersion = 1

type ChannelSnapshot struct {
	FormatVersion int                     `json:"format_version"`
	SMTP          *SnapshotSMTPChannel    `json:"smtp,omitempty"`
	Webhooks      []SnapshotWebhookTarget `json:"webhooks"`
}

type SnapshotSMTPChannel struct {
	Enabled        bool    `json:"enabled"`
	Host           string  `json:"host"`
	Port           int32   `json:"port"`
	From           string  `json:"from"`
	Recipient      string  `json:"recipient"`
	AuthType       string  `json:"auth_type"`
	Username       *string `json:"username,omitempty"`
	AuthCiphertext []byte  `json:"auth_ciphertext,omitempty"`
	AuthKeyVersion *int32  `json:"auth_key_version,omitempty"`
	TLSMode        string  `json:"tls_mode"`
}

type SnapshotWebhookTarget struct {
	ID                        string `json:"id"`
	Name                      string `json:"name"`
	Enabled                   bool   `json:"enabled"`
	URL                       string `json:"url"`
	SigningValueCiphertext    []byte `json:"signing_value_ciphertext"`
	SignatureHeaderCiphertext []byte `json:"signature_header_ciphertext"`
	SigningKeyVersion         int32  `json:"signing_key_version"`
}

type ChannelSnapshotStore struct {
	path string
}

func NewChannelSnapshotStore(path string) *ChannelSnapshotStore {
	return &ChannelSnapshotStore{path: path}
}

func (store *ChannelSnapshotStore) Sync(ctx context.Context, database DBTX) error {
	snapshot, err := loadChannelSnapshotFromDatabase(ctx, database)
	if err != nil {
		return err
	}
	return store.Write(snapshot)
}

func (store *ChannelSnapshotStore) Write(snapshot ChannelSnapshot) error {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode notification channel snapshot: %w", err)
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(store.path)
	file, err := os.CreateTemp(directory, ".notification-channels-")
	if err != nil {
		return fmt.Errorf("create notification channel snapshot: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("set notification channel snapshot permissions: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		file.Close()
		return fmt.Errorf("write notification channel snapshot: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync notification channel snapshot: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close notification channel snapshot: %w", err)
	}
	if err := os.Rename(temporary, store.path); err != nil {
		return fmt.Errorf("publish notification channel snapshot: %w", err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open notification channel snapshot directory: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync notification channel snapshot directory: %w", err)
	}
	return nil
}

func (store *ChannelSnapshotStore) Load() (ChannelSnapshot, error) {
	contents, err := os.ReadFile(store.path)
	if err != nil {
		return ChannelSnapshot{}, fmt.Errorf("read notification channel snapshot: %w", err)
	}
	var snapshot ChannelSnapshot
	if err := json.Unmarshal(contents, &snapshot); err != nil {
		return ChannelSnapshot{}, fmt.Errorf("decode notification channel snapshot: %w", err)
	}
	if snapshot.FormatVersion != ChannelSnapshotFormatVersion {
		return ChannelSnapshot{}, fmt.Errorf("unsupported notification channel snapshot format %d", snapshot.FormatVersion)
	}
	return snapshot, nil
}

func loadChannelSnapshotFromDatabase(ctx context.Context, database DBTX) (ChannelSnapshot, error) {
	queries := New(database)
	snapshot := ChannelSnapshot{FormatVersion: ChannelSnapshotFormatVersion, Webhooks: []SnapshotWebhookTarget{}}
	smtp, err := queries.GetSMTPChannel(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ChannelSnapshot{}, fmt.Errorf("read SMTP channel for snapshot: %w", err)
	}
	if err == nil {
		snapshot.SMTP = &SnapshotSMTPChannel{
			Enabled: smtp.Enabled, Host: smtp.Host, Port: smtp.Port, From: smtp.FromAddress,
			Recipient: smtp.Recipient, AuthType: smtp.AuthType,
			AuthCiphertext: append([]byte(nil), smtp.AuthCiphertext...), TLSMode: smtp.TlsMode,
		}
		if smtp.Username.Valid {
			username := smtp.Username.String
			snapshot.SMTP.Username = &username
		}
		if smtp.AuthKeyVersion.Valid {
			version := smtp.AuthKeyVersion.Int32
			snapshot.SMTP.AuthKeyVersion = &version
		}
	}
	webhooks, err := queries.ListWebhookTargets(ctx)
	if err != nil {
		return ChannelSnapshot{}, fmt.Errorf("read Webhook targets for snapshot: %w", err)
	}
	for _, target := range webhooks {
		snapshot.Webhooks = append(snapshot.Webhooks, SnapshotWebhookTarget{
			ID: uuid.UUID(target.ID.Bytes).String(), Name: target.Name, Enabled: target.Enabled, URL: target.Url,
			SigningValueCiphertext:    append([]byte(nil), target.SigningValueCiphertext...),
			SignatureHeaderCiphertext: append([]byte(nil), target.SignatureHeaderCiphertext...),
			SigningKeyVersion:         target.SigningKeyVersion,
		})
	}
	return snapshot, nil
}
