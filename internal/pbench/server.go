package pbench

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/evidence"
	"fmt"
	"io"
	"net"
	"time"
)

const MaxMessage = 16384

// bindBudget covers the whole connection, not only HandshakeContext. Stop
// waits for an already-running cancellation callback before releasing it.
func bindBudget(ctx context.Context, c net.Conn) (func(), error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(60 * time.Second)
	}
	if e := c.SetDeadline(deadline); e != nil {
		return nil, e
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = c.Close(); close(done) })
	return func() {
		if !stop() {
			<-done
		}
	}, nil
}

func readMessage(r *bufio.Reader, dst any) ([]byte, error) {
	var line []byte
	for {
		part, e := r.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > MaxMessage {
			return nil, fmt.Errorf("P message exceeds length limit")
		}
		if e == bufio.ErrBufferFull {
			continue
		}
		if e != nil {
			if e == io.EOF && len(line) > 0 {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, e
		}
		return line, evidence.Decode(line, dst)
	}
}
func writeMessage(w io.Writer, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	b = append(b, '\n')
	n, e := w.Write(b)
	if e == nil && n != len(b) {
		return io.ErrShortWrite
	}
	return e
}

// ServeOne requires one complete telemetry/control/ACK exchange. Clean EOF
// before that exchange is a failure. ServeConnection permits clean EOF only at
// the next telemetry boundary after all outstanding ACKs have been validated.
func ServeOne(ctx context.Context, ln net.Listener, session *app.ControlSession, expectDevice string) (TLSFacts, error) {
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()
	c, e := ln.Accept()
	if e != nil {
		return TLSFacts{}, e
	}
	defer c.Close()
	return serveTLS(ctx, c, session, expectDevice, true, "", "", nil)
}
func ServeConnection(ctx context.Context, c net.Conn, session *app.ControlSession, expectDevice string) (TLSFacts, error) {
	return serveTLS(ctx, c, session, expectDevice, false, "", "", nil)
}

func serveTLS(ctx context.Context, c net.Conn, session *app.ControlSession, expectDevice string, once bool, run, sid string, emit func(evidence.BusinessEvent) error) (TLSFacts, error) {
	var facts TLSFacts
	defer c.Close()
	tc, ok := c.(*tls.Conn)
	if !ok {
		return facts, fmt.Errorf("P requires TLS connection")
	}
	stop, e := bindBudget(ctx, c)
	if e != nil {
		return facts, e
	}
	defer stop()
	if e = tc.HandshakeContext(ctx); e != nil {
		return facts, fmt.Errorf("server handshake: %w", e)
	}
	facts = factsOf(tc.ConnectionState())
	if e = evidence.CheckTLS(&facts, expectDevice); e != nil {
		return facts, e
	}
	r := bufio.NewReaderSize(tc, 4096)
	seen := map[uint64]bool{}
	count := 0
	publish := func(tx evidence.Transaction, outcome string) error {
		if emit == nil {
			return nil
		}
		return emit(evidence.EventOf(tx, outcome))
	}
	for {
		var tel app.Telemetry
		b, e := readMessage(r, &tel)
		if e != nil {
			if e == io.EOF && !once && count > 0 {
				return facts, nil
			}
			return facts, fmt.Errorf("telemetry: %w", e)
		}
		if e = app.ValidateTelemetry(tel, facts.PeerCommonName); e != nil {
			return facts, e
		}
		if tel.SampleID == 0 || seen[tel.SampleID] || len(seen) >= 10000 {
			return facts, fmt.Errorf("invalid/duplicate sample")
		}
		seen[tel.SampleID] = true
		tx := evidence.Transaction{RunID: run, PeerID: tel.DeviceID, SessionID: sid, SampleID: tel.SampleID, OperationID: fmt.Sprintf("set-interval-%s-%d", tel.DeviceID, tel.SampleID), PlaintextBytes: int64(len(b) - 1)}
		if e = publish(tx, "telemetry-accepted"); e != nil {
			return facts, e
		}
		ctrl := app.Control{OperationID: tx.OperationID, SamplingIntervalMS: 5000}
		if e = writeMessage(tc, ctrl); e != nil {
			return facts, fmt.Errorf("control: %w", e)
		}
		if e = publish(tx, "control-sent"); e != nil {
			return facts, e
		}
		var ack app.Ack
		if _, e = readMessage(r, &ack); e != nil {
			return facts, fmt.Errorf("ack: %w", e)
		}
		if ack.OperationID != ctrl.OperationID || !ack.Applied || ack.SamplingIntervalMS != ctrl.SamplingIntervalMS {
			return facts, fmt.Errorf("ACK operation/applied/actual interval mismatch")
		}
		if e = publish(tx, "control-confirmed"); e != nil {
			return facts, e
		}
		count++
		if once {
			return facts, nil
		}
	}
}
