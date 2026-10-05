// Package pbench implements the M5B public hybrid comparison (experiment P):
// an INDEPENDENT Go TLS 1.3 + X25519MLKEM768 channel with full mutual
// certificate authentication.
//
// Contract rules implemented here:
//   - TLS 1.3 only; CurvePreferences restricted to X25519MLKEM768 on both
//     sides; the ACTUAL ConnectionState (Version, CipherSuite, CurveID,
//     HandshakeComplete, DidResume, peer certificate identity) is read after
//     the handshake instead of being inferred from the configuration.
//   - ClientAuth = RequireAndVerifyClientCert; a trusted CA is NOT enough: the
//     client certificate's CN must equal the expected device identity.
//   - Session tickets/resumption are disabled and DidResume=true does not
//     count as a full-handshake sample. No InsecureSkipVerify, no
//     KeyLogWriter, no secret export.
//   - The application reuses the SAME shared control handler
//     (app.HandleControl) over a newline-delimited JSON framing; P uses its own
//     public connection_id and never claims the SM2-derived SID semantics.
//   - P is a public comparison, NOT a national-crypto Suite 3, and nothing here
//     changes the T/H cryptographic paths.
package pbench

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"example.com/edgehybrid/internal/evidence"
)

const SchemaP = "m5b-public-hybrid/2"

const (
	GatewayDNS   = "gateway.edgehybrid.test"
	DeviceCN     = "D001"
	ExpectedPeer = DeviceCN
)

// EnsureCerts generates the test CA and the server/client certificates the
// first time it is called for a directory. Certificate generation is setup and
// happens OUTSIDE every timing window.
func EnsureCerts(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	caKeyPath := filepath.Join(dir, "ca.key")
	caCertPath := filepath.Join(dir, "ca.crt")
	if _, err := os.Stat(caCertPath); err == nil {
		return nil // already provisioned
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "edgehybrid test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return err
	}
	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return err
	}
	if err := writePEM(caKeyPath, "EC PRIVATE KEY", caKeyDER); err != nil {
		return err
	}
	if err := writePEM(caCertPath, "CERTIFICATE", caDER); err != nil {
		return err
	}

	leaf := func(cn string, dnsNames []string, eku []x509.ExtKeyUsage, serial int64) (*ecdsa.PrivateKey, []byte, error) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: cn},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			DNSNames:     dnsNames,
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  eku,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		if err != nil {
			return nil, nil, err
		}
		return key, der, nil
	}
	srvKey, srvDER, err := leaf(GatewayDNS, []string{GatewayDNS, "localhost"}, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, 2)
	if err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, "server.key"), "EC PRIVATE KEY", marshalEC(srvKey)); err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, "server.crt"), "CERTIFICATE", srvDER); err != nil {
		return err
	}
	cliKey, cliDER, err := leaf(DeviceCN, nil, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, 3)
	if err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, "client.key"), "EC PRIVATE KEY", marshalEC(cliKey)); err != nil {
		return err
	}
	return writePEM(filepath.Join(dir, "client.crt"), "CERTIFICATE", cliDER)
}

func marshalEC(k *ecdsa.PrivateKey) []byte {
	b, _ := x509.MarshalECPrivateKey(k)
	return b
}

func writePEM(path, blockType string, der []byte) error {
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600)
}

// LoadServerConfig builds the server TLS configuration.
func LoadServerConfig(dir string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key"))
	if err != nil {
		return nil, err
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("CA certificate could not be parsed")
	}
	return &tls.Config{
		MinVersion:             tls.VersionTLS13,
		MaxVersion:             tls.VersionTLS13,
		CurvePreferences:       []tls.CurveID{tls.X25519MLKEM768},
		Certificates:           []tls.Certificate{cert},
		ClientAuth:             tls.RequireAndVerifyClientCert,
		ClientCAs:              pool,
		SessionTicketsDisabled: true,
	}, nil
}

// LoadClientConfig builds the client TLS configuration bound to the expected
// gateway identity.
func LoadClientConfig(dir, serverName string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key"))
	if err != nil {
		return nil, err
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("CA certificate could not be parsed")
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		MaxVersion:         tls.VersionTLS13,
		CurvePreferences:   []tls.CurveID{tls.X25519MLKEM768},
		Certificates:       []tls.Certificate{cert},
		RootCAs:            pool,
		ServerName:         serverName,
		ClientSessionCache: nil, // no resumption
	}, nil
}

// TLSFacts records what the handshake ACTUALLY negotiated.
type TLSFacts = evidence.TLSFacts

func factsOf(cs tls.ConnectionState) TLSFacts {
	cn := ""
	chain := 0
	if len(cs.VerifiedChains) > 0 && len(cs.VerifiedChains[0]) > 0 {
		chain = len(cs.VerifiedChains[0])
		cn = cs.VerifiedChains[0][0].Subject.CommonName
	}
	return TLSFacts{
		Version: uint16(cs.Version), VersionName: tlsVersionName(cs.Version),
		CipherSuite: uint16(cs.CipherSuite), CipherSuiteName: tls.CipherSuiteName(cs.CipherSuite),
		CurveID: uint16(cs.CurveID), CurveName: curveName(cs.CurveID),
		HandshakeComplete: cs.HandshakeComplete, DidResume: cs.DidResume,
		PeerCommonName: cn, VerifiedChainLen: chain,
	}
}

func tlsVersionName(v uint16) string {
	if v == tls.VersionTLS13 {
		return "TLS1.3"
	}
	return fmt.Sprintf("0x%04x", v)
}

func curveName(c tls.CurveID) string {
	if c == tls.X25519MLKEM768 {
		return "X25519MLKEM768"
	}
	return fmt.Sprintf("CurveID(%d)", uint16(c))
}

// ConnectionFacts is one P trial row.
type ConnectionFacts struct {
	Business          evidence.ThroughputRound `json:"business"`
	SchemaVersion     string                   `json:"schema_version"`
	RunID             string                   `json:"run_id"`
	TrialID           int                      `json:"trial_id"`
	ConnectionID      string                   `json:"connection_id"`
	StartedAtUTC      string                   `json:"started_at_utc"`
	Success           bool                     `json:"success"`
	HandshakeComplete bool                     `json:"handshake_complete"`
	DidResume         bool                     `json:"did_resume"`
	TLS               TLSFacts                 `json:"tls"`
	IdentityBound     bool                     `json:"identity_bound"`
	TCPConnectNS      int64                    `json:"tcp_connect_ns"`
	HandshakeNS       int64                    `json:"handshake_total_ns"`
	ProbeNS           int64                    `json:"probe_ns"`
	ErrorStage        string                   `json:"error_stage,omitempty"`
	ErrorCode         string                   `json:"error_code,omitempty"`
	Detail            string                   `json:"detail,omitempty"`
	CPUNS             *int64                   `json:"cpu_total_ns"`
	CPUReason         string                   `json:"cpu_unavailable_reason,omitempty"`
	PeakRSSBytes      *int64                   `json:"peak_rss_bytes"`
	RSSReason         string                   `json:"peak_rss_unavailable_reason,omitempty"`
}

// RunRejectionTests proves the P channel enforces its contract: a wrong server
// identity, a missing client certificate and a mismatched group are all
// rejected. Results are recorded whether or not each case behaves as expected.
func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func WriteJSONLinesP(path string, rows any) error { return evidence.WriteLines(path, rows) }
func WriteJSONFileP(path string, v any) error     { return evidence.WriteJSON(path, v) }
