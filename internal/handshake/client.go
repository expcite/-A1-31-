package handshake

import (
	"context"
	"fmt"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/wire"
)

// Client runs the device side: SM2 initiator, msg1 sender, msg3 sender.
//
// The returned Channel exists only after the gateway's signature AND the
// gateway's Finished have both verified. On any failure the connection is
// closed, so a caller cannot accidentally continue on a half-authenticated
// socket.
func Client(ctx context.Context, c Config) (Channel, error) {
	ch, err := clientHandshake(ctx, c)
	if err != nil {
		// c.Conn may be nil: configuration validation runs before any I/O, and a
		// caller that passed no connection still deserves a clean error rather
		// than a panic on the error path.
		if c.Conn != nil {
			_ = c.Conn.Close()
		}
		return nil, err
	}
	return ch, nil
}

func clientHandshake(ctx context.Context, c Config) (Channel, error) {
	r, err := c.prepare(identity.RoleDevice)
	if err != nil {
		return nil, err
	}
	if !identity.ValidName(c.ExpectedPeerID) {
		return nil, fmt.Errorf("%w: an expected gateway identity is required", ErrConfig)
	}
	peer, err := r.lookup(ctx, c.ExpectedPeerID, identity.RoleGateway)
	if err != nil {
		return nil, err
	}

	ag := cryptosuite.NewSM2Agreement()
	eph, err := ag.GenerateEphemeral()
	if err != nil {
		return nil, fmt.Errorf("%w: ephemeral SM2 key: %v", ErrProtocol, err)
	}
	defer func() { _ = eph.Private.Destroy() }()
	if len(eph.Public) != SM2PointSize {
		return nil, fmt.Errorf("%w: ephemeral SM2 key has length %d", ErrProtocol, len(eph.Public))
	}

	na, err := randomBytes(NonceSize)
	if err != nil {
		return nil, err
	}

	// ONE absolute deadline for the whole handshake, taken from the earlier of the
	// caller's deadline and the local budget. Messages never extend it.
	fio, err := newFrameIO(ctx, c.Conn, c.MaxFrame, c.Timeout)
	if err != nil {
		return nil, err
	}

	// --- ML-KEM-768 ephemeral key (suite 2 only) ----------------------------
	//
	// Generated per handshake, never cached and never reused: a reused
	// decapsulation key would make every session that used it share one PQ
	// secret. The key is only created when suite 2 is actually OFFERED, and the
	// server enforces the matching presence rule.
	var kemK cryptosuite.KEM
	var kemPriv cryptosuite.PrivateHandle
	var kemPub []byte
	if offersHybrid(r.u16) {
		kemK = c.kem()
		pub, priv, kerr := kemK.Generate()
		if kerr != nil {
			return nil, fmt.Errorf("%w: generating the ML-KEM ephemeral key: %v", ErrProtocol, kerr)
		}
		kemPub, kemPriv = pub, priv
		defer func() { _ = kemPriv.Destroy() }()
	}

	// --- msg1: ClientHello ---------------------------------------------------
	hello := ClientHelloBody{
		OfferedSuites:       r.u16,
		InitiatorID:         c.Local.ID,
		InitiatorSignKeyID:  c.Local.SignKeyID,
		InitiatorAgreeKeyID: c.Local.AgreeKeyID,
		ResponderID:         c.ExpectedPeerID,
		Nonce:               na,
		EphemeralSM2:        eph.Public,
		EphemeralKEM:        kemPub, // empty unless suite 2 is offered
	}
	if err := checkClientKEMField(r.u16, kemPub); err != nil {
		return nil, err
	}
	m1, err := fio.send(wire.ClientHello, hello.encode())
	if err != nil {
		return nil, err
	}

	// --- msg2: ServerHello + gateway signature -------------------------------
	payload2, m2, err := fio.recv(wire.ServerHello)
	if err != nil {
		return nil, err
	}
	coreBytes, sigB, err := splitServerHello(payload2)
	if err != nil {
		return nil, fmt.Errorf("%w: ServerHello is too short", ErrProtocol)
	}
	core, err := parseServerCore(coreBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: ServerCore: %v", ErrProtocol, err)
	}
	if core.InitiatorID != c.Local.ID || core.ResponderID != peer.Public.ID {
		return nil, fmt.Errorf("%w: ServerHello names initiator %q and responder %q", ErrPeerIdentity, core.InitiatorID, core.ResponderID)
	}
	if core.ResponderSignKeyID != peer.Public.SignKeyID || core.ResponderAgreeKeyID != peer.Public.AgreementKeyID {
		return nil, fmt.Errorf("%w: gateway presented key identifiers the registry does not hold", ErrPeerIdentity)
	}
	// Recompute the selection from the server's advertised list and the TRUSTED
	// minimum modes. If the server picked anything else, the handshake stops
	// here rather than accepting a suite the client never agreed to.
	//
	// This is one of the two structural downgrade defences: the client does not
	// simply accept whatever the server announced. An attacker who deletes the
	// hybrid entry from H1 makes the SERVER select suite 1, but the client
	// recomputes suite 2 from its own configuration and rejects the mismatch.
	sel, err := policy.Select(r.suites, toSuites(core.SupportedSuites), c.MinimumMode, peer.MinimumMode, c.AllowLegacy)
	if err != nil {
		return nil, fmt.Errorf("%w: no mutually acceptable suite: %v", ErrUnsupported, err)
	}
	if uint16(sel) != core.SelectedSuite {
		return nil, fmt.Errorf("%w: gateway selected suite %d but %d was expected", ErrProtocol, core.SelectedSuite, uint16(sel))
	}
	// The ciphertext rule follows what was SELECTED, not what was offered.
	if err := checkServerKEMField(core.SelectedSuite, core.KEMCiphertext); err != nil {
		return nil, err
	}
	// A hybrid selection is only meaningful if this client actually offered it.
	if core.SelectedSuite == SuiteHybrid && !offersHybrid(r.u16) {
		return nil, fmt.Errorf("%w: gateway selected the hybrid suite although it was never offered", ErrProtocol)
	}
	if err := cryptosuite.NewSM2Identity().Verify(
		peer.Public.SignPublic, peer.Public.SM2UserID,
		serverAuthMessage(m1, coreBytes), sigB,
	); err != nil {
		return nil, fmt.Errorf("%w: gateway signature: %v", ErrAuthentication, err)
	}

	// --- SM2 key exchange and key schedule ----------------------------------
	peerID := cryptosuite.IdentityPublic{
		ID:              peer.Public.ID,
		SM2UserID:       peer.Public.SM2UserID,
		AgreementPublic: peer.Public.AgreementPublic,
	}
	localID := cryptosuite.IdentityPublic{ID: c.Local.ID, SM2UserID: c.Local.SM2UserID}
	if ph, ok := c.Local.AgreeKey.(cryptosuite.PublicHandle); ok {
		if pub, perr := ph.Public(); perr == nil {
			localID.AgreementPublic = pub
		}
	}
	res, err := ag.Derive(cryptosuite.Initiator, localID, c.Local.AgreeKey, eph, peerID, core.EphemeralSM2)
	if err != nil {
		return nil, fmt.Errorf("%w: SM2 key exchange: %w", ErrProtocol, err)
	}
	defer clear(res.SharedSecret)
	defer clear(res.SendConfirmation)
	defer clear(res.ExpectedPeerConfirmation)
	if len(res.ExpectedPeerConfirmation) != cryptosuite.SM3Size || !cryptosuite.ConstantTimeEqual(core.SM2Confirmation, res.ExpectedPeerConfirmation) {
		return nil, fmt.Errorf("%w: gateway S1", ErrSM2Confirmation)
	}

	// --- ML-KEM-768 decapsulation (suite 2 only) ----------------------------
	//
	// FIPS 203 implicit rejection: a ciphertext of the correct length that is
	// NOT a genuine encryption returns a DIFFERENT pseudorandom secret with
	// err == nil. So a nil error here proves nothing about authenticity; only
	// the Finished values can detect a substituted ciphertext.
	var zpq []byte
	if core.SelectedSuite == SuiteHybrid {
		if kemK == nil || kemPriv == nil {
			return nil, fmt.Errorf("%w: the hybrid suite was selected but no ML-KEM key was generated", ErrProtocol)
		}
		zpq, err = kemK.Decapsulate(kemPriv, core.KEMCiphertext)
		if err != nil {
			return nil, fmt.Errorf("%w: ML-KEM decapsulation: %v", ErrProtocol, err)
		}
	}

	kikm, err := ikm(core.SelectedSuite, res.SharedSecret, zpq)
	if err != nil {
		return nil, err
	}
	slt, err := salt(na, core.Nonce)
	if err != nil {
		return nil, err
	}
	prk := cryptosuite.HKDFExtractSM3(slt, kikm)
	t2 := transcriptHashT2(m1, m2)
	kfinA, kfinB, err := finishedKeys(prk, t2)
	if err != nil {
		return nil, err
	}

	// --- msg3: ClientCore + signature + FinishedA ---------------------------
	cc := ClientCoreBody{
		SelectedSuite:       core.SelectedSuite,
		InitiatorID:         c.Local.ID,
		InitiatorSignKeyID:  c.Local.SignKeyID,
		InitiatorAgreeKeyID: c.Local.AgreeKeyID,
		SM2Confirmation:     res.SendConfirmation,
	}
	ccBytes := cc.encode()
	sigA, err := cryptosuite.NewSM2Identity().Sign(
		c.Local.SignKey, c.Local.SM2UserID,
		clientAuthMessage(m1, m2, ccBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: signing ClientAuth: %v", ErrProtocol, err)
	}
	fa := confirmA(kfinA, m1, m2, ccBytes, sigA)
	payload3 := make([]byte, 0, len(ccBytes)+len(sigA)+len(fa))
	payload3 = append(payload3, ccBytes...)
	payload3 = append(payload3, sigA...)
	payload3 = append(payload3, fa...)
	m3, err := fio.send(wire.ClientAuth, payload3)
	if err != nil {
		return nil, err
	}

	// --- msg4: ServerFinished ------------------------------------------------
	payload4, m4, err := fio.recv(wire.ServerFinished)
	if err != nil {
		return nil, err
	}
	if len(payload4) != FinishedSize {
		return nil, fmt.Errorf("%w: FinishedB has length %d", ErrProtocol, len(payload4))
	}
	wantB := confirmB(kfinB, m1, m2, m3)
	if !cryptosuite.ConstantTimeEqual(payload4, wantB) {
		return nil, fmt.Errorf("%w: gateway Finished did not match", ErrTranscript)
	}

	// Only now — after both signatures, both confirmations and the complete
	// transcript — may session keys be derived and a Channel be handed out.
	t4 := transcriptHashT4(m1, m2, m3, m4)
	sm, err := sessionKeys(prk, t4)
	if err != nil {
		return nil, err
	}
	snap := PeerSnapshot{
		PeerID:           peer.Public.ID,
		SignKeyID:        peer.Public.SignKeyID,
		AgreementKeyID:   peer.Public.AgreementKeyID,
		KeyGeneration:    peer.KeyGeneration,
		RegistryRevision: peer.Revision,
	}
	ch, err := newChannel(c.Conn, c.MaxFrame, c.Timeout, sm, core.SelectedSuite, peer.Public.ID, snap, true, r.limit, r.budget)
	return profileChannel(ch, err, c.IdleTimeout)
}
