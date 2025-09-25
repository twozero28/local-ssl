package certs

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	caCertFile     = "devlink-ca.pem"
	caKeyFile      = "devlink-ca.key"
	serverCertFile = "devlink-localhost.pem"
	serverKeyFile  = "devlink-localhost.key"
)

// Manager handles creation and persistence of the local certificate authority
// and issued certificates.
type Manager struct {
	dir string
}

// NewManager creates a new certificate manager using the provided state
// directory.
func NewManager(dir string) (*Manager, error) {
	if dir == "" {
		return nil, errors.New("state directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	return &Manager{dir: dir}, nil
}

// EnsureCertificate ensures the TLS certificate for *.localhost exists and
// returns it.
func (m *Manager) EnsureCertificate() (*tls.Certificate, error) {
	if err := m.ensureCA(); err != nil {
		return nil, err
	}
	if err := m.ensureServerCert(); err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(m.serverCertPath(), m.serverKeyPath())
	if err != nil {
		return nil, fmt.Errorf("load server cert: %w", err)
	}
	return &cert, nil
}

func (m *Manager) ensureCA() error {
	certPath := m.caCertPath()
	keyPath := m.caKeyPath()
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return nil
		}
	}

	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("generate CA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Devlink Local CA"},
			CommonName:   "Devlink Local CA",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create CA certificate: %w", err)
	}

	if err := writePEM(certPath, "CERTIFICATE", der); err != nil {
		return err
	}
	if err := writePEM(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key)); err != nil {
		return err
	}

	return nil
}

func (m *Manager) ensureServerCert() error {
	certPath := m.serverCertPath()
	keyPath := m.serverKeyPath()
	caCert, caKey, err := m.loadCA()
	if err != nil {
		return err
	}

	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if len(cert.Certificate) > 0 {
			parsed, err := x509.ParseCertificate(cert.Certificate[0])
			if err == nil && time.Now().Before(parsed.NotAfter.Add(-30*24*time.Hour)) {
				return nil
			}
		}
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate server key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Devlink Localhost"},
		DNSNames:     []string{"localhost", "*.localhost"},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().AddDate(3, 0, 0),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("issue server certificate: %w", err)
	}

	if err := writePEM(certPath, "CERTIFICATE", der); err != nil {
		return err
	}
	if err := writePEM(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key)); err != nil {
		return err
	}
	return nil
}

func (m *Manager) loadCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(m.caCertPath())
	if err != nil {
		return nil, nil, fmt.Errorf("read CA cert: %w", err)
	}
	keyPEM, err := os.ReadFile(m.caKeyPath())
	if err != nil {
		return nil, nil, fmt.Errorf("read CA key: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, nil, errors.New("invalid CA certificate encoding")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA cert: %w", err)
	}

	block, _ = pem.Decode(keyPEM)
	if block == nil {
		return nil, nil, errors.New("invalid CA key encoding")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA key: %w", err)
	}

	return cert, key, nil
}

func (m *Manager) caCertPath() string {
	return filepath.Join(m.dir, caCertFile)
}

func (m *Manager) caKeyPath() string {
	return filepath.Join(m.dir, caKeyFile)
}

func (m *Manager) serverCertPath() string {
	return filepath.Join(m.dir, serverCertFile)
}

func (m *Manager) serverKeyPath() string {
	return filepath.Join(m.dir, serverKeyFile)
}

func writePEM(path, typ string, der []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	if err := pem.Encode(file, &pem.Block{Type: typ, Bytes: der}); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return nil
}
