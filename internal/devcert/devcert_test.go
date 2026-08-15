package devcert

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsure(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "server.pem")
	key := filepath.Join(dir, "server-key.pem")
	ca, generated, err := Ensure(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	if !generated {
		t.Fatal("first call must generate")
	}
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = leaf.VerifyHostname("localhost"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(ca); err != nil {
		t.Fatal(err)
	}
	_, generated, err = Ensure(cert, key)
	if err != nil || generated {
		t.Fatalf("second call generated=%v err=%v", generated, err)
	}
}
