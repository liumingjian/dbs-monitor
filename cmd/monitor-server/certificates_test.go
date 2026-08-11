package main

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCertificatesReplacesMismatchedSAN(t *testing.T) {
	directory := t.TempDir()
	if _, _, err := ensureCertificates(directory, "localhost"); err != nil {
		t.Fatalf("create first certificate: %v", err)
	}
	certificatePath, _, err := ensureCertificates(directory, "127.0.0.1")
	if err != nil {
		t.Fatalf("replace certificate: %v", err)
	}
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		t.Fatalf("read replaced certificate: %v", err)
	}
	block, _ := pem.Decode(certificatePEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse replaced certificate: %v", err)
	}
	if err := certificate.VerifyHostname("127.0.0.1"); err != nil {
		t.Fatalf("replaced certificate SAN: %v", err)
	}
}

func TestEnsureCertificatesCreatesVerifiableSANAndPrivateKey(t *testing.T) {
	directory := t.TempDir()
	certificatePath, keyPath, err := ensureCertificates(directory, "127.0.0.1")
	if err != nil {
		t.Fatalf("ensure certificates: %v", err)
	}
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		t.Fatalf("read certificate: %v", err)
	}
	block, chain := pem.Decode(certificatePEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	if err := certificate.VerifyHostname("127.0.0.1"); err != nil {
		t.Fatalf("verify certificate SAN: %v", err)
	}
	caBlock, _ := pem.Decode(chain)
	if caBlock == nil {
		t.Fatal("server certificate does not include the CA chain")
	}
	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil || !ca.IsCA {
		t.Fatalf("server certificate chain CA = %+v, error %v", ca, err)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("private key permissions = %o, want 600", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(directory, "ca.crt")); err != nil {
		t.Fatalf("CA certificate missing: %v", err)
	}
}

func TestEnsureCertificatesRepairsMissingCAChain(t *testing.T) {
	directory := t.TempDir()
	certificatePath, _, err := ensureCertificates(directory, "127.0.0.1")
	if err != nil {
		t.Fatalf("create certificates: %v", err)
	}
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		t.Fatalf("read certificate: %v", err)
	}
	leaf, _ := pem.Decode(certificatePEM)
	if leaf == nil {
		t.Fatal("server certificate is not PEM encoded")
	}
	if err := os.WriteFile(certificatePath, pem.EncodeToMemory(leaf), 0644); err != nil {
		t.Fatalf("remove certificate chain: %v", err)
	}

	if _, _, err := ensureCertificates(directory, "127.0.0.1"); err != nil {
		t.Fatalf("repair certificate chain: %v", err)
	}
	repairedPEM, err := os.ReadFile(certificatePath)
	if err != nil {
		t.Fatalf("read repaired certificate: %v", err)
	}
	_, chain := pem.Decode(repairedPEM)
	ca, _ := pem.Decode(chain)
	if ca == nil {
		t.Fatal("repaired server certificate does not include the CA chain")
	}
}
