package broker

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

type tlsMaterial struct {
	CertPEM  []byte
	KeyPEM   []byte
	PKCS12   []byte
	Password string
}

func generateTLS(commonName string, dnsNames []string) (tlsMaterial, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tlsMaterial{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tlsMaterial{}, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName, Organization: []string{"osb-service"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return tlsMaterial{}, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return tlsMaterial{}, err
	}
	password, err := randomPassword(24)
	if err != nil {
		return tlsMaterial{}, err
	}
	pfx, err := pkcs12.Legacy.Encode(key, cert, nil, password)
	if err != nil {
		return tlsMaterial{}, fmt.Errorf("pkcs12: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tlsMaterial{}, err
	}
	return tlsMaterial{
		CertPEM:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:   pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		PKCS12:   pfx,
		Password: password,
	}, nil
}
