// Package lab runs plaintext loopback transport tests only. There is no identity
// verification, cryptographic handshake or secure performance claim here.
package lab

import (
	"context"
	"example.com/edgehybrid/internal/wire"
	"fmt"
	"net"
	"sync"
	"time"
)

type Summary struct {
	SecurityStatus    string  `json:"security_status"`
	Devices           int     `json:"devices"`
	MessagesPerDevice int     `json:"messages_per_device"`
	Delivered         int     `json:"delivered"`
	DurationMS        float64 `json:"plaintext_transport_duration_ms"`
}

func Run(ctx context.Context, devices, messages int) (Summary, error) {
	var out Summary
	if devices < 1 || devices > 200 || messages < 1 || messages > 1000 {
		return out, fmt.Errorf("devices must be 1..200, messages must be 1..1000")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return out, e
	}
	defer ln.Close()
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	started := time.Now()
	serverDone := make(chan error, 1)
	go func() {
		var wg sync.WaitGroup
		errs := make(chan error, devices+1)
		for i := 0; i < devices; i++ {
			c, err := ln.Accept()
			if err != nil {
				errs <- err
				break
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				_ = c.SetDeadline(deadline)
				for j := 0; j < messages; j++ {
					f, err := wire.Read(c, 2048)
					if err != nil {
						errs <- err
						return
					}
					if f.Kind != wire.LabTelemetry {
						errs <- fmt.Errorf("unexpected lab message")
						return
					}
					if err = wire.Write(c, wire.Frame{Kind: wire.LabAck, Payload: f.Payload}, 2048); err != nil {
						errs <- err
						return
					}
				}
			}(c)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()
	var wg sync.WaitGroup
	errs := make(chan error, devices)
	counts := make(chan int, devices)
	for i := 0; i < devices; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var d net.Dialer
			c, err := d.DialContext(ctx, "tcp", ln.Addr().String())
			if err != nil {
				errs <- err
				return
			}
			defer c.Close()
			_ = c.SetDeadline(deadline)
			for j := 0; j < messages; j++ {
				body := []byte(fmt.Sprintf("device=D%03d sample=%d transport-lab-only", id+1, j))
				if err = wire.Write(c, wire.Frame{Kind: wire.LabTelemetry, Payload: body}, 2048); err != nil {
					errs <- err
					return
				}
				f, err := wire.Read(c, 2048)
				if err != nil {
					errs <- err
					return
				}
				if f.Kind != wire.LabAck || string(f.Payload) != string(body) {
					errs <- fmt.Errorf("unexpected lab ack")
					return
				}
			}
			counts <- messages
		}(i)
	}
	wg.Wait()
	close(errs)
	close(counts)
	for err := range errs {
		if err != nil {
			cancel()
			_ = ln.Close()
			<-serverDone
			return out, err
		}
	}
	if e := <-serverDone; e != nil {
		return out, e
	}
	out = Summary{SecurityStatus: "PLAINTEXT_LOOPBACK_LAB_ONLY_NOT_A_SECURE_CHANNEL", Devices: devices, MessagesPerDevice: messages, DurationMS: float64(time.Since(started).Microseconds()) / 1000}
	for n := range counts {
		out.Delivered += n
	}
	return out, nil
}
