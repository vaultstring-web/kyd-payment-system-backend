package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"os"
	"time"
)

func main() {
	if err := os.MkdirAll("certs", 0755); err != nil {
		log.Fatalf("Failed to create certs directory: %v", err)
	}

	// 1. Generate CA
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(2024),
		Subject: pkix.Name{
			Organization: []string{"KYD Payment System"},
			CommonName:   "KYD Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	caPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		log.Fatal(err)
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		log.Fatal(err)
	}

	pemEncode("certs/ca.crt", "CERTIFICATE", caBytes)
	pemEncode("certs/ca.key", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(caPrivKey))

	// 2. Generate Server Cert (Payment Service)
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(2025),
		Subject: pkix.Name{
			Organization: []string{"KYD Payment System"},
			CommonName:   "payment-service",
		},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	serverPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		log.Fatal(err)
	}

	serverBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &serverPrivKey.PublicKey, caPrivKey)
	if err != nil {
		log.Fatal(err)
	}

	pemEncode("certs/server.crt", "CERTIFICATE", serverBytes)
	pemEncode("certs/server.key", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(serverPrivKey))

	// 3. Generate Client Cert (Gateway)
	clientCert := &x509.Certificate{
		SerialNumber: big.NewInt(2026),
		Subject: pkix.Name{
			Organization: []string{"KYD Payment System"},
			CommonName:   "gateway-client",
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		SubjectKeyId: []byte{1, 2, 3, 4, 7},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	clientPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		log.Fatal(err)
	}

	clientBytes, err := x509.CreateCertificate(rand.Reader, clientCert, ca, &clientPrivKey.PublicKey, caPrivKey)
	if err != nil {
		log.Fatal(err)
	}

	pemEncode("certs/client.crt", "CERTIFICATE", clientBytes)
	pemEncode("certs/client.key", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(clientPrivKey))

	log.Println("Certificates generated in certs/ directory")
}

func pemEncode(path, typeName string, bytes []byte) {
	out, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	pem.Encode(out, &pem.Block{
		Type:  typeName,
		Bytes: bytes,
	})
}
