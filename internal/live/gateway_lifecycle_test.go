package live

import (
	"context"
	"encoding/json"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/management"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGatewayRotationRealTCPGenerations(t *testing.T) {
	for _, n := range []int{1, 20} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			m := testManager(t, n, Options{})
			online(t, m, n)
			before := m.Snapshot()
			oldTemp := 26.3
			m.mu.Lock()
			m.operations["old-gw-request"] = &Operation{Run: m.run, ID: "old-gw-request", Device: "D001", SID: before.Devices[0].SID, Kind: "telemetry", Status: "queued", Temperature: &oldTemp, Queued: time.Now().UTC()}
			m.mu.Unlock()
			oldRuntime, e := m.runtime("GW001", "gateway")
			if e != nil {
				t.Fatal(e)
			}
			defer oldRuntime.Close()
			if _, e = m.Manage("GW001", "rotate", "", "rotate:GW001"); e == nil {
				t.Fatal("没有全部设备二次确认")
			}
			r, e := m.Manage("GW001", "rotate", "", "rotate:GW001:ALL")
			if e != nil {
				t.Fatal(e)
			}
			if r["all_sessions_closed"] != true || r["admission_reopened"] != true || m.Online() != 0 {
				t.Fatal("旧会话未清理", r)
			}
			next := m.Snapshot().Gateway
			if next.Generation != before.Gateway.Generation+1 || next.SignKey == before.Gateway.SignKey || next.AgreementKey == before.Gateway.AgreementKey {
				t.Fatal("网关世代未切换")
			}
			online(t, m, n)
			pauseSampling(t, m)
			currentSID := m.Snapshot().Devices[0].SID
			m.mu.Lock()
			m.devices["D001"].requests <- localRequest{kind: "telemetry", operation: "old-gw-request", temperature: oldTemp, run: m.run, device: "D001", session: currentSID}
			m.mu.Unlock()
			for i, d := range m.Snapshot().Devices {
				if d.Suite != 2 || d.SID == before.Devices[i].SID {
					t.Fatal("旧会话复用")
				}
				if _, e = m.Telemetry(d.ID, "newgw-"+d.ID, 26.3); e != nil {
					t.Fatal(e)
				}
				confirmed(t, m, "newgw-"+d.ID)
				if _, e = m.Control(d.ID, "newgw-control-"+d.ID, 5000); e != nil {
					t.Fatal(e)
				}
				confirmed(t, m, "newgw-control-"+d.ID)
			}
			// The client evidence is its immutable authenticated peer snapshot, not the
			// current registry copied into an earlier session after rotation.
			seen := map[string]bool{}
			oldOp, _ := m.Operation("old-gw-request")
			if oldOp.SID != before.Devices[0].SID || oldOp.Status != "failed" || oldOp.Written != nil || oldOp.Confirmed != nil {
				t.Fatal("网关维护后旧请求复活", oldOp)
			}
			for _, ev := range m.Snapshot().Events {
				if ev.Endpoint == "device" && ev.Stage == "authenticated" && ev.GatewaySnapshot != nil && ev.GatewaySnapshot.KeyGeneration == next.Generation {
					seen[ev.Device] = true
				}
			}
			if len(seen) != n {
				t.Fatal("新世代双端握手证据不全")
			}
			// Same gateway identity with held old private handles is refused over TCP.
			ln, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer ln.Close()
			rejected := make(chan error, 1)
			go func() {
				c, e := ln.Accept()
				if e != nil {
					rejected <- e
					return
				}
				defer c.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				ch, e := handshake.Server(ctx, m.hs(oldRuntime, c))
				if ch != nil {
					ch.Close()
				}
				rejected <- e
			}()
			rt, e := m.runtime("D001", "device")
			if e != nil {
				t.Fatal(e)
			}
			defer rt.Close()
			conn, e := net.DialTimeout("tcp4", ln.Addr().String(), time.Second)
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ch, e := handshake.Client(ctx, m.hs(rt, conn))
			if ch != nil {
				ch.Close()
			}
			if e == nil {
				t.Fatal("旧网关同ID密钥被客户端接受")
			}
			if e = <-rejected; e == nil {
				t.Fatal("旧网关握手未失败")
			}
			b, _ := json.Marshal(r)
			t.Logf("GW_LIFECYCLE run=%s count=%d result=%s", m.RunID(), n, b)
		})
	}
}

func TestGatewayRevocationRetirementPersistsOnRestart(t *testing.T) {
	base := t.TempDir()
	o := Options{PrivateDir: filepath.Join(base, "private"), PublicDir: filepath.Join(base, "logs"), Count: 1}
	m, e := New(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	online(t, m, 1)
	if _, e = m.Manage("GW001", "revoke", "", "revoke:GW001:ALL"); e != nil {
		t.Fatal(e)
	}
	if m.Online() != 0 || m.Connect("D001") == nil {
		t.Fatal("吊销后仍可连接")
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	m, e = New(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	if m.Snapshot().Gateway.Lifecycle != identity.LifecycleRevoked || m.Connect("D001") == nil {
		t.Fatal("重启复活吊销网关")
	}
	if _, e = m.Manage("GW001", "rotate", "", "rotate:GW001:ALL"); e == nil {
		t.Fatal("轮换自动恢复吊销")
	}
	if _, e = m.Manage("GW001", "retire", "", "retire:GW001:ALL"); e != nil {
		t.Fatal(e)
	}
	ids, e := identity.CurrentKeyIDs(filepath.Join(m.private, "keys"), "GW001")
	if e != nil || len(ids) != 0 {
		t.Fatal("退役清理失败", ids, e)
	}
	if m.Snapshot().Gateway.Lifecycle != identity.LifecycleRetired || m.Connect("D001") == nil {
		t.Fatal("退役准入错误")
	}
	t.Logf("GW_REVOKE_RESTART run=%s revoked persisted; retired key files removed", m.RunID())
}

func TestGatewayMaintenanceFailureSafety(t *testing.T) {
	t.Run("commit_file_error", func(t *testing.T) {
		m := testManager(t, 1, Options{})
		keyDir := filepath.Join(m.private, "keys")
		plan, e := management.PrepareGateway(m.reg, keyDir, "GW001", "rotate")
		if e != nil {
			t.Fatal(e)
		}
		trust := filepath.Join(m.private, "trust.json")
		saved := trust + ".saved"
		if e = os.Rename(trust, saved); e != nil {
			t.Fatal(e)
		}
		if e = os.Mkdir(trust, 0700); e != nil {
			t.Fatal(e)
		}
		block := filepath.Join(trust, "block")
		if e = os.WriteFile(block, []byte("public fixture"), 0600); e != nil {
			t.Fatal(e)
		}
		_, e = plan.Commit(m.reg)
		if e == nil {
			t.Fatal("实际提交写入错误未拒绝")
		}
		os.Remove(block)
		os.Remove(trust)
		if e = os.Rename(saved, trust); e != nil {
			t.Fatal(e)
		}
		p, _ := identity.Metadata(m.reg, "GW001")
		if p.KeyGeneration != plan.Before.KeyGeneration {
			t.Fatal("文件提交失败修改可信世代")
		}
		online(t, m, 1)
		if _, e = m.Manage("GW001", "cleanup", "", "cleanup:GW001:ALL"); e != nil {
			t.Fatal(e)
		}
		ids, e := identity.CurrentKeyIDs(keyDir, "GW001")
		if e != nil || len(ids) != 2 {
			t.Fatal("孤立准备文件清理错误", ids, e)
		}
	})
	t.Run("preparation", func(t *testing.T) {
		m := testManager(t, 1, Options{})
		online(t, m, 1)
		before := m.Snapshot().Gateway
		// An invalid keystore directory causes a real filesystem preparation error.
		keyDir := filepath.Join(m.private, "keys")
		oldDir := keyDir + "-saved"
		if e := os.Rename(keyDir, oldDir); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(keyDir, []byte("blocked"), 0600); e != nil {
			t.Fatal(e)
		}
		_, e := m.Manage("GW001", "rotate", "", "rotate:GW001:ALL")
		if e == nil {
			t.Fatal("文件失败未拒绝")
		}
		os.Remove(keyDir)
		os.Rename(oldDir, keyDir)
		if m.Snapshot().Gateway.Generation != before.Generation {
			t.Fatal("准备失败改变旧可信世代")
		}
		if e = m.Disconnect("D001"); e != nil {
			t.Fatal(e)
		}
		online(t, m, 1)
	})
	t.Run("stale_CAS", func(t *testing.T) {
		m := testManager(t, 1, Options{})
		keyDir := filepath.Join(m.private, "keys")
		p, e := management.PrepareGateway(m.reg, keyDir, "GW001", "rotate")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = identity.Revoke(m.reg, "GW001"); e != nil {
			t.Fatal(e)
		}
		if _, e = p.Commit(m.reg); e == nil {
			t.Fatal("交错吊销被覆盖")
		}
		if m.Connect("D001") == nil {
			t.Fatal("CAS失败复活网关")
		}
	})
	t.Run("committed_load_failure", func(t *testing.T) {
		m := testManager(t, 1, Options{})
		keyDir := filepath.Join(m.private, "keys")
		p, e := management.PrepareGateway(m.reg, keyDir, "GW001", "rotate")
		if e != nil {
			t.Fatal(e)
		}
		m.maintenancePhase("committing", true, nil)
		r, e := p.Commit(m.reg)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.Remove(filepath.Join(keyDir, "GW001", p.Next.Public.SignKeyID+".key")); e != nil {
			t.Fatal(e)
		}
		result, e := m.adoptGateway(p, r)
		if e != nil || result["admission_reopened"] != false || m.Connect("D001") == nil {
			t.Fatal("提交后加载失败未关闭准入", result, e)
		}
		now, _ := identity.Metadata(m.reg, "GW001")
		if now.KeyGeneration != p.Next.KeyGeneration {
			t.Fatal("加载失败回滚旧密钥")
		}
	})
	t.Run("cleanup_pending", func(t *testing.T) {
		m := testManager(t, 1, Options{})
		keyDir := filepath.Join(m.private, "keys")
		p, e := management.PrepareGateway(m.reg, keyDir, "GW001", "rotate")
		if e != nil {
			t.Fatal(e)
		}
		r, e := p.Commit(m.reg)
		if e != nil {
			t.Fatal(e)
		}
		path := filepath.Join(keyDir, "GW001", p.Before.Public.SignKeyID+".key")
		if e = os.Remove(path); e != nil {
			t.Fatal(e)
		}
		if e = os.Mkdir(path, 0700); e != nil {
			t.Fatal(e)
		}
		os.WriteFile(filepath.Join(path, "block"), []byte("public test fixture"), 0600)
		result, e := m.adoptGateway(p, r)
		if e != nil || result["cleanup_pending"] != true || result["admission_reopened"] != true {
			t.Fatal("清理失败没有分离提交和清理", result, e)
		}
		online(t, m, 1)
		if _, e = m.Telemetry("D001", "cleanup-positive", 28.7); e != nil {
			t.Fatal(e)
		}
		confirmed(t, m, "cleanup-positive")
	})
}
