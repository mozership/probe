package grpc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCert writes a self-signed certificate and its key as PEM files in dir
// and returns their paths.
func writeCert(t *testing.T, dir, name string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, name+".crt")
	keyPath := filepath.Join(dir, name+".key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestTLSConfig(t *testing.T) {
	dir := t.TempDir()
	caCert, _ := writeCert(t, dir, "ca")
	clientCert, clientKey := writeCert(t, dir, "client")
	_, otherKey := writeCert(t, dir, "other")
	garbage := filepath.Join(dir, "garbage.pem")
	if err := os.WriteFile(garbage, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.pem")

	tests := []struct {
		name     string
		req      Req
		wantErr  string
		roots    bool
		certs    int
		skipping bool
	}{
		{name: "none", req: Req{}},
		{name: "ca ok", req: Req{CAFile: caCert}, roots: true},
		{name: "ca missing", req: Req{CAFile: missing}, wantErr: "failed to read CA file"},
		{name: "ca garbage", req: Req{CAFile: garbage}, wantErr: "failed to parse CA certificate"},
		{name: "cert and key", req: Req{CertFile: clientCert, KeyFile: clientKey}, certs: 1},
		{name: "cert only", req: Req{CertFile: clientCert}, wantErr: "cert_file and key_file must be given together"},
		{name: "key only", req: Req{KeyFile: clientKey}, wantErr: "cert_file and key_file must be given together"},
		{name: "bad keypair", req: Req{CertFile: clientCert, KeyFile: otherKey}, wantErr: "failed to load client certificate"},
		{name: "insecure", req: Req{Insecure: true}, skipping: true},
		{name: "insecure with cert", req: Req{Insecure: true, CertFile: clientCert, KeyFile: clientKey}, certs: 1, skipping: true},
		{name: "insecure with cert only", req: Req{Insecure: true, CertFile: clientCert}, wantErr: "cert_file and key_file must be given together"},
		{name: "insecure ignores ca", req: Req{Insecure: true, CAFile: missing}, skipping: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := tt.req.tlsConfig()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := cfg.RootCAs != nil; got != tt.roots {
				t.Errorf("RootCAs set = %v, want %v", got, tt.roots)
			}
			if got := len(cfg.Certificates); got != tt.certs {
				t.Errorf("len(Certificates) = %d, want %d", got, tt.certs)
			}
			if cfg.InsecureSkipVerify != tt.skipping {
				t.Errorf("InsecureSkipVerify = %v, want %v", cfg.InsecureSkipVerify, tt.skipping)
			}
		})
	}
}
