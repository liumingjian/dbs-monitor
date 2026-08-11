package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func ensureCertificates(directory, publicHost string) (string, string, error) {
	caPath := filepath.Join(directory, "ca.crt")
	certificatePath := filepath.Join(directory, "server.crt")
	keyPath := filepath.Join(directory, "server.key")
	if certificateMatchesHost(certificatePath, keyPath, publicHost) {
		if err := ensureCertificateChain(certificatePath, caPath); err != nil {
			return "", "", err
		}
		return certificatePath, keyPath, nil
	}
	if publicHost == "" {
		return "", "", fmt.Errorf("PUBLIC_HOST is required to create the TLS certificate SAN")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", "", err
	}

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	caSerial, err := serial()
	if err != nil {
		return "", "", err
	}
	caTemplate := x509.Certificate{SerialNumber: caSerial, Subject: pkix.Name{CommonName: "dbs-monitor CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return "", "", err
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		return "", "", err
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serverSerial, err := serial()
	if err != nil {
		return "", "", err
	}
	serverTemplate := x509.Certificate{SerialNumber: serverSerial, Subject: pkix.Name{CommonName: publicHost}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(2, 0, 0), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	if ip := net.ParseIP(publicHost); ip != nil {
		serverTemplate.IPAddresses = []net.IP{ip}
	} else {
		serverTemplate.DNSNames = []string{publicHost}
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, caCertificate, &serverKey.PublicKey, caKey)
	if err != nil {
		return "", "", err
	}
	if err := writePEM(caPath, "CERTIFICATE", caDER, 0644); err != nil {
		return "", "", err
	}
	if err := writeCertificateChain(certificatePath, serverDER, caDER); err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		return "", "", err
	}
	if err := writePEM(keyPath, "PRIVATE KEY", keyDER, 0600); err != nil {
		return "", "", err
	}
	return certificatePath, keyPath, nil
}

func ensureCertificateChain(certificatePath, caPath string) error {
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		return err
	}
	_, remainder := pem.Decode(certificatePEM)
	if block, _ := pem.Decode(remainder); block != nil {
		return nil
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return fmt.Errorf("read TLS CA for certificate chain: %w", err)
	}
	file, err := os.OpenFile(certificatePath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	if len(certificatePEM) > 0 && certificatePEM[len(certificatePEM)-1] != '\n' {
		if _, err := file.Write([]byte("\n")); err != nil {
			return err
		}
	}
	_, err = file.Write(caPEM)
	return err
}

func writeCertificateChain(path string, certificates ...[]byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, certificate := range certificates {
		if err := pem.Encode(file, &pem.Block{Type: "CERTIFICATE", Bytes: certificate}); err != nil {
			return err
		}
	}
	return nil
}

func certificateMatchesHost(certificatePath, keyPath, publicHost string) bool {
	if publicHost == "" {
		return false
	}
	if _, err := os.Stat(keyPath); err != nil {
		return false
	}
	contents, err := os.ReadFile(certificatePath)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(contents)
	if block == nil {
		return false
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	return err == nil && certificate.VerifyHostname(publicHost) == nil
}

func certificateExpiration(certificatePath string) (*time.Time, error) {
	contents, err := os.ReadFile(certificatePath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(contents)
	if block == nil {
		return nil, fmt.Errorf("decode TLS certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	expiresAt := certificate.NotAfter.UTC()
	return &expiresAt, nil
}

func writePEM(path, kind string, contents []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer file.Close()
	return pem.Encode(file, &pem.Block{Type: kind, Bytes: contents})
}

func serial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}
