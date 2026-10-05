package cryptosuite

// Exported validators so the trusted registry and the handshake can reject a
// malformed or off-curve public key BEFORE it reaches any protocol logic.
//
// These are the same checks the adapters apply internally; exposing them lets
// provisioning code refuse to write a trust entry it could never verify against.

// ValidSigningPublic reports whether b is a canonical uncompressed SM2 point
// that lies on the SM2 curve and can be used for signature verification.
func ValidSigningPublic(b []byte) bool {
	_, err := parseSM2PublicKey(b)
	return err == nil
}

// ValidAgreementPublic reports whether b is a canonical uncompressed point
// accepted by the SM2 key-exchange curve group.
//
// Note this is an ENCODING and on-curve check only. It does not and cannot
// establish that the key was honestly generated or that the generator had good
// randomness — see docs/M1_ERRATA.md.
func ValidAgreementPublic(b []byte) bool {
	_, err := parseECDHPublicKey(b)
	return err == nil
}
