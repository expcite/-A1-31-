package console

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDynamicDeviceAddition(t *testing.T) {
	s, client, csrf := serverFixture(t)

	// 1. Initially 1 device
	snap := s.Manager.Snapshot()
	if len(snap.Devices) != 1 || snap.Devices[0].ID != "D001" {
		t.Fatalf("expected 1 device (D001), got %d devices", len(snap.Devices))
	}

	// 2. Add device auto (empty body {}) -> should allocate D002
	req, _ := http.NewRequest("POST", s.URL+"/api/v1/devices", strings.NewReader(`{}`))
	req.Header.Set("Origin", s.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201 Created, got %d: %s", resp.StatusCode, string(body))
	}
	var created struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID != "D002" || created.Role != "device" {
		t.Fatalf("unexpected created device: %+v", created)
	}

	// Verify snapshot has D001 and D002
	snap = s.Manager.Snapshot()
	if len(snap.Devices) != 2 || snap.Devices[0].ID != "D001" || snap.Devices[1].ID != "D002" {
		t.Fatalf("expected [D001, D002], got: %+v", snap.Devices)
	}

	// 3. Add device explicit: D005
	req, _ = http.NewRequest("POST", s.URL+"/api/v1/devices", strings.NewReader(`{"device_id":"D005"}`))
	req.Header.Set("Origin", s.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for D005, got %d", resp.StatusCode)
	}

	// Verify snapshot sorted order: D001, D002, D005
	snap = s.Manager.Snapshot()
	if len(snap.Devices) != 3 || snap.Devices[0].ID != "D001" || snap.Devices[1].ID != "D002" || snap.Devices[2].ID != "D005" {
		t.Fatalf("expected sorted [D001, D002, D005], got: %+v", snap.Devices)
	}

	// 4. Duplicate D005 should fail with 409
	code := doRequest(t, s, client, "POST", "/api/v1/devices", `{"device_id":"D005"}`, s.URL, csrf)
	if code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate D005, got %d", code)
	}

	// 5. Unregistered device D999 should fail with 409
	code = doRequest(t, s, client, "POST", "/api/v1/devices", `{"device_id":"D999"}`, s.URL, csrf)
	if code != http.StatusConflict {
		t.Fatalf("expected 409 for unregistered D999, got %d", code)
	}

	// 6. Unknown JSON field should fail with 400
	code = doRequest(t, s, client, "POST", "/api/v1/devices", `{"unknown_field":"value"}`, s.URL, csrf)
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d", code)
	}

	// 7. Newly added device D002 can be connected
	code = doRequest(t, s, client, "POST", "/api/v1/devices/D002/connect", `{}`, s.URL, csrf)
	if code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for D002 connect, got %d", code)
	}

	// 8. Add remaining devices until 20
	for i := 3; i <= 20; i++ {
		id := fmt.Sprintf("D%03d", i)
		if id == "D005" {
			continue // already added
		}
		c := doRequest(t, s, client, "POST", "/api/v1/devices", fmt.Sprintf(`{"device_id":%q}`, id), s.URL, csrf)
		if c != http.StatusCreated {
			t.Fatalf("failed to add %s, got HTTP %d", id, c)
		}
	}
	snap = s.Manager.Snapshot()
	if len(snap.Devices) != 20 {
		t.Fatalf("expected 20 devices in pool, got %d", len(snap.Devices))
	}

	// 9. Adding 21st device must fail with 409 limit
	code = doRequest(t, s, client, "POST", "/api/v1/devices", `{}`, s.URL, csrf)
	if code != http.StatusConflict {
		t.Fatalf("expected 409 when pool is full, got %d", code)
	}
}
