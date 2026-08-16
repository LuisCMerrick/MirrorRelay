// Package devcert generates self-signed certificates for development.
package devcert

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

func Ensure(certPath, keyPath string) (caPath string, generated bool, err error) {
	caPath = filepath.Join(filepath.Dir(certPath), "dev-ca.pem")
	if fileExists(certPath) && fileExists(keyPath) && fileExists(caPath) {
		return caPath, false, nil
	}
	if err = os.MkdirAll(filepath.Dir(certPath), 0o750); err != nil {
		return caPath, false, err
	}
	now := time.Now().Add(-5 * time.Minute)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return caPath, false, err
	}
	caTemplate := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "MirrorRelay Development CA"},
		NotBefore: now, NotAfter: now.Add(10 * 365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return caPath, false, err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return caPath, false, err
	}
	leafTemplate := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: now, NotAfter: now.Add(825 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		return caPath, false, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		return caPath, false, err
	}
	if err = writePEM(caPath, 0o644, "CERTIFICATE", caDER); err != nil {
		return caPath, false, err
	}
	if err = writePEM(certPath, 0o644, "CERTIFICATE", appendPEM(leafDER, caDER)); err != nil {
		return caPath, false, err
	}
	if err = writePEM(keyPath, 0o600, "PRIVATE KEY", keyDER); err != nil {
		return caPath, false, err
	}
	return caPath, true, nil
}

func serial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return n
}
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
func appendPEM(values ...[]byte) []byte {
	var out []byte
	for _, v := range values {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: v})...)
	}
	return out
}
func writePEM(path string, mode os.FileMode, typ string, der []byte) error {
	var data []byte
	if typ == "CERTIFICATE" && len(der) > 0 && der[0] == '-' {
		data = der
	} else {
		data = pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return fmt.Errorf("install certificate: %w", err)
	}
	return nil
}
