package evidence

type TLSFacts struct {
	Version           uint16 `json:"version"`
	VersionName       string `json:"version_name"`
	CipherSuite       uint16 `json:"cipher_suite"`
	CipherSuiteName   string `json:"cipher_suite_name"`
	CurveID           uint16 `json:"curve_id"`
	CurveName         string `json:"curve_name"`
	HandshakeComplete bool   `json:"handshake_complete"`
	DidResume         bool   `json:"did_resume"`
	PeerCommonName    string `json:"peer_common_name"`
	VerifiedChainLen  int    `json:"verified_chain_len"`
}
