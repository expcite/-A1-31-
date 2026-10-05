package main

import (
	"context"
	"encoding/json"
	"example.com/edgehybrid/internal/lab"
	"flag"
	"fmt"
	"os"
)

func main() {
	n := flag.Int("devices", 20, "loopback simulated devices (1..200)")
	m := flag.Int("messages", 3, "messages per device (1..1000)")
	flag.Parse()
	fmt.Fprintln(os.Stderr, "WARNING: plaintext loopback transport lab; no cryptographic handshake, no authentication.")
	s, e := lab.Run(context.Background(), *n, *m)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if e = json.NewEncoder(os.Stdout).Encode(s); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
