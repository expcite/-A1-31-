package evidence

// ThroughputRoundRow is one round of real business transactions.
type ThroughputRound struct {
	StartedAtUTC        string        `json:"formal_started_at_utc,omitempty"`
	Suite               uint16        `json:"suite"`
	AuthenticatedPeerID string        `json:"authenticated_peer_id"`
	WireC2SBase         int64         `json:"wire_c2s_base"`
	WireS2CBase         int64         `json:"wire_s2c_base"`
	WireC2SEnd          int64         `json:"wire_c2s_end"`
	WireS2CEnd          int64         `json:"wire_s2c_end"`
	TCPConnectNS        int64         `json:"tcp_connect_ns"`
	HandshakeNS         int64         `json:"handshake_total_ns"`
	ServerError         string        `json:"server_error,omitempty"`
	SchemaVersion       string        `json:"schema_version"`
	RunID               string        `json:"run_id"`
	Mode                string        `json:"mode"`
	PeerID              string        `json:"peer_id"`
	Transactions        []Transaction `json:"transactions"`
	ServerTLS           *TLSFacts     `json:"server_tls,omitempty"`
	ClientTLS           *TLSFacts     `json:"client_tls,omitempty"`
	ConfirmationMethod  string        `json:"confirmation_method"`
	Round               int           `json:"round"`
	SessionID           string        `json:"session_id,omitempty"`
	Attempted           int           `json:"attempted"`
	// Completed requires remote ACK validation AND its local observation.
	// Accepted, control sent, ACK sent and remote confirmation are separate facts.
	CompletedTransactions int `json:"completed_transactions"`
	FailedTransactions    int `json:"failed_transactions"`
	// Goodput numerator: per-message plaintext lengths of exactly the accepted
	// telemetry records, accumulated one by one (G1: no perMsgLen x count).
	AcceptedTelemetryPlaintextBytes int64 `json:"accepted_telemetry_plaintext_bytes"`
	AcceptedCount                   int   `json:"accepted_count"`
	// Windows (device-side, one monotonic clock):
	//   accept_confirmed: first send -> last control received (remote acceptance
	//   observed). ack_send: first send -> last ACK written; a DIFFERENT
	//   measurement, never called "confirmed".
	AcceptConfirmedWindowNS       int64    `json:"accept_confirmed_window_ns"`
	AckSendWindowNS               int64    `json:"ack_send_window_ns"`
	GatewayAcceptedCount          int      `json:"gateway_accepted_count"`
	GatewaySentCount              int      `json:"gateway_sent_count"`
	EndToEndConfirmedWindowNS     int64    `json:"end_to_end_confirmed_window_ns"`
	RateUnavailableReason         string   `json:"rate_unavailable_reason"`
	GatewayConfirmedCount         int      `json:"gateway_confirmed_count"`
	ApplicationGoodputBytesPerSec *float64 `json:"application_goodput_bytes_per_sec"`
	TransactionsPerSec            *float64 `json:"transactions_per_sec"`
	// Wire-level counters: handshake-excluded differences for both directions.
	WireClientToServerBytes int64  `json:"wire_client_to_server_bytes"`
	WireServerToClientBytes int64  `json:"wire_server_to_client_bytes"`
	GatewayReceiveWindowNS  *int64 `json:"gateway_receive_window_ns"`
	GatewayWindowReason     string `json:"gateway_window_reason,omitempty"`
	FormalResultsEligible   bool   `json:"formal_results_eligible"`
	NotEligibleReason       string `json:"formal_not_eligible_reason"`
	ErrorStage              string `json:"error_stage,omitempty"`
	Detail                  string `json:"detail,omitempty"`
}

// ThroughputSummary aggregates the rounds from the raw rows.
type ThroughputSummary struct {
	RateUnavailableReason            string   `json:"rate_unavailable_reason"`
	SchemaVersion                    string   `json:"schema_version"`
	RunID                            string   `json:"run_id"`
	Mode                             string   `json:"mode"`
	Rounds                           int      `json:"rounds"`
	PerRound                         int      `json:"per_round"`
	TotalAttempted                   int      `json:"total_attempted"`
	TotalCompleted                   int      `json:"total_completed"`
	TotalFailed                      int      `json:"total_failed"`
	TotalGoodputBytes                int64    `json:"total_accepted_telemetry_plaintext_bytes"`
	AvgApplicationGoodputBytesPerSec *float64 `json:"avg_application_goodput_bytes_per_sec"`
	AvgTransactionsPerSec            *float64 `json:"avg_transactions_per_sec"`
	FormalResultsEligible            bool     `json:"formal_results_eligible"`
	NotEligibleReason                string   `json:"formal_not_eligible_reason"`
}
