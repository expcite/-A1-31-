'use strict';
const $ = id => document.getElementById(id);
let last = null;
function text(id, value) { $(id).textContent = String(value); }
function badge(status) { const b = document.createElement('span'); b.className = 'badge' + (status === '已完成本轮' ? ' done' : status === '失败' ? ' fail' : status === '已停止' ? ' off' : ''); b.textContent = status; return b; }
function render(s) {
  document.body.classList.remove('disconnected');
  last = s;
  text('phase', s.status); $('phase').className = badge(s.status).className;
  text('connection', '本次状态页已连接 · ' + new Date().toLocaleTimeString('zh-CN'));
  text('device-count', s.expected_devices); text('online', s.online); text('handshakes', s.handshake_devices);
  text('accepted', s.accepted); text('sent', s.sent); text('confirmed', s.confirmed);
  text('gateway', s.gateway_id + ' · ' + s.gateway_status + (s.gateway_address ? ' · ' + s.gateway_address : ''));
  text('workers', '已完成 ' + s.completed_devices + ' / ' + s.expected_devices + ' · 失败 ' + s.failed_devices);
  text('run-id', s.run_id); text('logs', s.log_directory);
  $('devices').replaceChildren(...s.devices.map(d => {
    const tr = document.createElement('tr'); tr.dataset.device = d.id;
    const fields = [d.id, '', d.suite === 2 ? 'Suite2 · KEM ' + (d.kem_used ? '已使用' : '未确认') : '尚未协商', d.session_id || '—', d.accepted + ' / ' + d.sent + ' / ' + d.confirmed];
    fields.forEach((value, i) => { const td = document.createElement('td'); td.textContent = value; if (i === 3) td.className = 'sid'; if (i === 1) { td.append(badge(d.status)); if (d.error) { const p = document.createElement('div'); p.className = 'detail'; p.textContent = d.error_stage + '：' + d.error; td.append(p); } } tr.append(td); });
    return tr;
  }));
  $('events').replaceChildren(...s.events.slice().reverse().map(item => {
    const row = document.createElement('div'); row.className = 'event';
    const time = document.createElement('time'); time.textContent = new Date(item.event.ts).toLocaleTimeString('zh-CN');
    const who = document.createElement('b'); who.textContent = item.source + (item.event.peer_id ? ' → ' + item.event.peer_id : '');
    const detail = document.createElement('span'); detail.textContent = item.event.stage + ' / ' + item.event.outcome + (item.event.operation_id ? ' · ' + item.event.operation_id : '');
    row.append(time, who, detail); return row;
  }));
  $('errors').replaceChildren(...s.errors.slice(-5).map(e => { const p = document.createElement('p'); p.textContent = e; return p; }));
}
function disconnected() {
  document.body.classList.add('disconnected');
  text('phase', last && last.status === '已停止' ? '已停止' : '页面连接已断开'); $('phase').className = 'badge off';
  text('connection', '无法确认当前在线状态；下方为最后缓存。程序可能已停止或异常退出。');
  text('online', '—');
  for (const b of document.querySelectorAll('#devices .badge')) { if (b.textContent === '通信中' || b.textContent === '连接中') { b.textContent = '状态不可确认'; b.className = 'badge off'; } }
}
async function poll() {
  const abort = new AbortController(); const timer = setTimeout(() => abort.abort(), 1800);
  try { const r = await fetch('/api/state', {cache: 'no-store', signal: abort.signal}); if (!r.ok) throw new Error('状态不可用'); const s = await r.json(); if (last && s.run_id !== last.run_id) throw new Error('拒绝另一运行的状态'); render(s); }
  catch (_) { disconnected(); }
  finally { clearTimeout(timer); setTimeout(poll, 500); }
}
poll();
