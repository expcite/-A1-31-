package handshake

import (
	"context"
	"fmt"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/wire"
)

// Server runs the gateway side: SM2 responder, msg2 sender, msg4 sender.
//
// The returned Channel exists only after the device's signature AND the
// device's FinishedA have verified AND msg4 has been fully written. Writing
// msg4 successfully does NOT mean the device received or accepted it; that
// remains unknown here, and nothing in this package claims otherwise.
func Server(ctx context.Context, c Config) (Channel, error) {
	ch, err := serverHandshake(ctx, c)
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

func serverHandshake(ctx context.Context, c Config) (Channel, error) {
	r, err := c.prepare(identity.RoleGateway)
	if err != nil {
		return nil, err
	}
	// ONE absolute deadline for the whole handshake, taken from the earlier of the
	// caller's deadline and the local budget. Messages never extend it.
	fio, err := newFrameIO(ctx, c.Conn, c.MaxFrame, c.Timeout)
	if err != nil {
		return nil, err
	}

	// --- msg1: ClientHello ---------------------------------------------------
	payload1, m1, err := fio.recv(wire.ClientHello)
	if err != nil {
		return nil, err
	}
	hello, err := parseClientHello(payload1)
	if err != nil {
		return nil, fmt.Errorf("%w: ClientHello: %v", ErrProtocol, err)
	}
	if hello.ResponderID != c.Local.ID {
		return nil, fmt.Errorf("%w: ClientHello is addressed to %q, not %q", ErrPeerIdentity, hello.ResponderID, c.Local.ID)
	}
	// The encapsulation key must match what this ClientHello ADVERTISES:
	// offering suite 2 requires a correctly sized key, offering only suite 1
	// requires the field to be empty. This is checked before the registry
	// lookup so a malformed advertisement cannot even reach that path.
	if err := checkClientKEMField(hello.OfferedSuites, hello.EphemeralKEM); err != nil {
		return nil, err
	}
	// The peer is resolved through the TRUSTED registry. A public key carried in
	// the message is never adopted; it must match what the registry already
	// holds for that identity, or the handshake stops.
	peer, err := r.lookup(ctx, hello.InitiatorID, identity.RoleDevice)
	if err != nil {
		return nil, err
	}
	if hello.InitiatorSignKeyID != peer.Public.SignKeyID || hello.InitiatorAgreeKeyID != peer.Public.AgreementKeyID {
		return nil, fmt.Errorf("%w: device presented key identifiers the registry does not hold", ErrPeerIdentity)
	}

	// Suite selection is provisional here and only becomes trustworthy once the
	// transcript is authenticated by the device's signature and FinishedA.
	sel, err := policy.Select(r.suites, toSuites(hello.OfferedSuites), c.MinimumMode, peer.MinimumMode, c.AllowLegacy)
	if err != nil {
		return nil, fmt.Errorf("%w: no mutually acceptable suite: %v", ErrUnsupported, err)
	}
	if err := requireSuite(uint16(sel)); err != nil {
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
	nb, err := randomBytes(NonceSize)
	if err != nil {
		return nil, err
	}

	// --- ML-KEM-768 encapsulation (suite 2 only) ----------------------------
	//
	// Real encapsulation against the key the device sent. The shared secret is
	// held for the key schedule; the ciphertext goes into ServerCore so that
	// SigB authenticates it and it becomes part of the transcript.
	var zpq, kemCT []byte
	if uint16(sel) == SuiteHybrid {
		zpq, kemCT, err = c.kem().Encapsulate(hello.EphemeralKEM)
		if err != nil {
			return nil, fmt.Errorf("%w: ML-KEM encapsulation: %v", ErrProtocol, err)
		}
		if len(kemCT) != cryptosuite.MLKEM768CiphertextSize {
			return nil, fmt.Errorf("%w: ML-KEM ciphertext has length %d",
				ErrProtocol, len(kemCT))
		}
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
	res, err := ag.Derive(cryptosuite.Responder, localID, c.Local.AgreeKey, eph, peerID, hello.EphemeralSM2)
	if err != nil {
		return nil, fmt.Errorf("%w: SM2 key exchange: %w", ErrProtocol, err)
	}
	defer clear(res.SharedSecret)
	defer clear(res.SendConfirmation)
	defer clear(res.ExpectedPeerConfirmation)

	// --- msg2: ServerCore + signature ---------------------------------------
	core := ServerCoreBody{
		SelectedSuite:       uint16(sel),
		SupportedSuites:     r.u16,
		ResponderID:         c.Local.ID,
		ResponderSignKeyID:  c.Local.SignKeyID,
		ResponderAgreeKeyID: c.Local.AgreeKeyID,
		InitiatorID:         hello.InitiatorID,
		Nonce:               nb,
		EphemeralSM2:        eph.Public,
		KEMCiphertext:       kemCT, // set only for suite 2
		SM2Confirmation:     res.SendConfirmation,
	}
	// Enforce the rule on what we are about to sign and send.
	if err := checkServerKEMField(uint16(sel), kemCT); err != nil {
		return nil, err
	}
	coreBytes := core.encode()
	sigB, err := cryptosuite.NewSM2Identity().Sign(
		c.Local.SignKey, c.Local.SM2UserID,
		serverAuthMessage(m1, coreBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: signing ServerHello: %v", ErrProtocol, err)
	}
	payload2 := make([]byte, 0, len(coreBytes)+len(sigB))
	payload2 = append(payload2, coreBytes...)
	payload2 = append(payload2, sigB...)
	m2, err := fio.send(wire.ServerHello, payload2)
	if err != nil {
		return nil, err
	}

	kikm, err := ikm(core.SelectedSuite, res.SharedSecret, zpq)
	if err != nil {
		return nil, err
	}
	slt, err := salt(hello.Nonce, nb)
	if err != nil {
		return nil, err
	}
	prk := cryptosuite.HKDFExtractSM3(slt, kikm)
	t2 := transcriptHashT2(m1, m2)
	kfinA, kfinB, err := finishedKeys(prk, t2)
	if err != nil {
		return nil, err
	}

	// --- msg3: verify the device --------------------------------------------
	payload3, m3, err := fio.recv(wire.ClientAuth)
	if err != nil {
		return nil, err
	}
	ccBytes, sigA, fa, err := splitClientAuth(payload3)
	if err != nil {
		return nil, fmt.Errorf("%w: ClientAuth is too short", ErrProtocol)
	}
	cc, err := parseClientCore(ccBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: ClientCore: %v", ErrProtocol, err)
	}
	if cc.InitiatorID != hello.InitiatorID || cc.InitiatorID != peer.Public.ID {
		return nil, fmt.Errorf("%w: ClientAuth names a different device", ErrPeerIdentity)
	}
	if cc.InitiatorSignKeyID != peer.Public.SignKeyID || cc.InitiatorAgreeKeyID != peer.Public.AgreementKeyID {
		return nil, fmt.Errorf("%w: ClientAuth presents unexpected key identifiers", ErrPeerIdentity)
	}
	if cc.SelectedSuite != core.SelectedSuite {
		return nil, fmt.Errorf("%w: device acknowledged suite %d, expected %d", ErrProtocol, cc.SelectedSuite, core.SelectedSuite)
	}
	if err := cryptosuite.NewSM2Identity().Verify(
		peer.Public.SignPublic, peer.Public.SM2UserID,
		clientAuthMessage(m1, m2, ccBytes), sigA,
	); err != nil {
		return nil, fmt.Errorf("%w: device signature: %v", ErrAuthentication, err)
	}
	if len(res.ExpectedPeerConfirmation) != cryptosuite.SM3Size || !cryptosuite.ConstantTimeEqual(cc.SM2Confirmation, res.ExpectedPeerConfirmation) {
		return nil, fmt.Errorf("%w: device S2", ErrSM2Confirmation)
	}
	wantA := confirmA(kfinA, m1, m2, ccBytes, sigA)
	if !cryptosuite.ConstantTimeEqual(fa, wantA) {
		return nil, fmt.Errorf("%w: device Finished did not match", ErrTranscript)
	}

	// --- msg4: ServerFinished ------------------------------------------------
	fb := confirmB(kfinB, m1, m2, m3)
	m4, err := fio.send(wire.ServerFinished, fb)
	if err != nil {
		return nil, err
	}

	// msg4 has been written. Only now does the gateway hold a usable session.
	t4 := transcriptHashT4(m1, m2, m3, m4)
	sm, err := sessionKeys(prk, t4)
	if err != nil {
		return nil, err
	}
	// Record what THIS handshake actually verified. The registry may be rotated
	// a moment later; the service compares this snapshot with the latest entry
	// before admitting business traffic, so a session authenticated on
	// generation N is never registered as N+1.
	snap := PeerSnapshot{
		PeerID:           peer.Public.ID,
		SignKeyID:        peer.Public.SignKeyID,
		AgreementKeyID:   peer.Public.AgreementKeyID,
		KeyGeneration:    peer.KeyGeneration,
		RegistryRevision: peer.Revision,
	}
	ch, err := newChannel(c.Conn, c.MaxFrame, c.Timeout, sm, core.SelectedSuite, peer.Public.ID, snap, false, r.limit, r.budget)
	return profileChannel(ch, err, c.IdleTimeout)
}
