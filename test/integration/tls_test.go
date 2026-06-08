package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// genTestCerts generates a self-signed CA + server certificate and key in a
// temporary directory.  It returns the directory path, the cert file path,
// the key file path, and the CA file path.
func genTestCerts(t *testing.T) (dir, certFile, keyFile, caFile string) {
	t.Helper()
	dir = t.TempDir()

	// --- CA key + cert ---
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	caSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate CA serial: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "hayakv-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}

	// Write CA cert PEM.
	caFile = filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER}), 0o644); err != nil {
		t.Fatalf("write CA cert: %v", err)
	}

	// Parse the CA cert so we can use it as the issuer below.
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}

	// --- Server key + cert (signed by the CA) ---
	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	srvSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate server serial: %v", err)
	}
	srvTemplate := &x509.Certificate{
		SerialNumber: srvSerial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	srvCertDER, err := x509.CreateCertificate(rand.Reader, srvTemplate, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create server cert: %v", err)
	}

	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvCertDER}), 0o644); err != nil {
		t.Fatalf("write server cert: %v", err)
	}
	srvKeyDER, err := x509.MarshalECPrivateKey(srvKey)
	if err != nil {
		t.Fatalf("marshal server key: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: srvKeyDER}), 0o600); err != nil {
		t.Fatalf("write server key: %v", err)
	}

	return dir, certFile, keyFile, caFile
}

// startHayakvTLS builds hayakv, writes a config with TLS enabled, and starts
// it.  It returns the TLS address and a stop function.
func startHayakvTLS(t *testing.T, certFile, keyFile, caFile string) (addr string, stop func()) {
	t.Helper()
	root := projectRoot(t)
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "hayakv")
	build := exec.Command("go", "build", "-o", bin, "./cmd/hayakv")
	build.Dir = root
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build hayakv: %v\n%s", err, out)
	}

	port := freePort(t) // plain TCP port (unused but required by config)
	tlsPort := freePort(t)
	addr = fmt.Sprintf("127.0.0.1:%d", tlsPort)

	conf := filepath.Join(tmp, "redis.conf")
	if err := os.WriteFile(conf, []byte(fmt.Sprintf(`bind 127.0.0.1
port %d
dir %s
databases 16
net goroutine
engine shardmap
proto-max resp2
tls-port %d
tls-cert-file %s
tls-key-file %s
tls-ca-cert-file %s
`, port, tmp, tlsPort, certFile, keyFile, caFile)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "CONFIG="+conf)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start hayakv: %v", err)
	}

	// Wait for the TLS port to be ready.
	waitForTLSReady(t, addr, caFile, certFile, keyFile)

	return addr, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
}

// waitForTLSReady polls until the TLS server responds to PING.
func waitForTLSReady(t *testing.T, addr, caFile, certFile, keyFile string) {
	t.Helper()

	// Load the CA cert so we can verify the server certificate.
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	// Load client cert (for mutual TLS if the server requires it).
	clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load client cert: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := tls.DialWithDialer(
			&net.Dialer{Timeout: 200 * time.Millisecond},
			"tcp", addr,
			&tls.Config{
				RootCAs:      pool,
				Certificates: []tls.Certificate{clientCert},
				ServerName:   "localhost",
			},
		)
		if err == nil {
			_, _ = conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
			_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			buf := make([]byte, 64)
			n, readErr := conn.Read(buf)
			_ = conn.Close()
			if readErr == nil && string(buf[:n]) == "+PONG\r\n" {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("TLS server at %s did not become ready", addr)
}

func TestTLSConnectivity(t *testing.T) {
	_, certFile, keyFile, caFile := genTestCerts(t)
	addr, stop := startHayakvTLS(t, certFile, keyFile, caFile)
	defer stop()

	// Load CA cert for the go-redis client.
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	client := redis.NewClient(&redis.Options{
		Addr: addr,
		TLSConfig: &tls.Config{
			RootCAs:    pool,
			ServerName: "localhost",
		},
	})
	defer client.Close()

	ctx := context.Background()

	if got := client.Ping(ctx).Val(); got != "PONG" {
		t.Fatalf("TLS PING = %q, want PONG", got)
	}
	if err := client.Set(ctx, "tls:key", "hello", 0).Err(); err != nil {
		t.Fatalf("TLS SET: %v", err)
	}
	if got := client.Get(ctx, "tls:key").Val(); got != "hello" {
		t.Fatalf("TLS GET = %q, want hello", got)
	}
}

func TestTLSMutualAuth(t *testing.T) {
	_, certFile, keyFile, caFile := genTestCerts(t)
	addr, stop := startHayakvTLS(t, certFile, keyFile, caFile)
	defer stop()

	// Load CA cert for the go-redis client.
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	// Load client cert (since the server requires client auth via CA cert).
	clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load client cert: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: addr,
		TLSConfig: &tls.Config{
			RootCAs:      pool,
			Certificates: []tls.Certificate{clientCert},
			ServerName:   "localhost",
		},
	})
	defer client.Close()

	ctx := context.Background()
	if got := client.Ping(ctx).Val(); got != "PONG" {
		t.Fatalf("mTLS PING = %q, want PONG", got)
	}
}
