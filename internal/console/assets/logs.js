import {node, card, button, table, detail, badge} from './components.js';
import {filterEvents, pageEvents} from './usability.js';

// One bounded view per API/run, including its at-most-2000-event snapshot.
// The application remounts pages on connectivity recovery. Retaining this view
// prevents a failed fetch followed by recovery from silently changing scope.
// A new run or API replaces it; no cross-run registry or persistent storage.
let lastView;

// Functional F07 repair only: the explicitly refreshed, unfiltered snapshot
// owns both the table/export range and all filter choices. No background refresh.
export function logs(initial, api) {
  if (lastView?.api === api && lastView.run === initial.run_id) {
    lastView.restore(); return lastView.view;
  }
  let snapshot = initial, page = 1;
  const all = node('div'), c = card('本次有界记录'), range = node('p');
  const filterBar = node('div', undefined, 'toolbar'), rows = node('div');
  const pages = node('p'), q = {}, selectors = new Map(), writes = [];
  const labels = {gateway:'网关 (gateway)', device:'设备 (device)', http:'管理接口 (http)', api:'操作排队 (api)',
    management:'密钥管理 (management)', authenticated:'安全握手已认证 (authenticated)', telemetry_accepted:'网关验收温度 (telemetry_accepted)',
    confirmed:'双端确认完成 (confirmed)', queued:'队列等待发送 (queued)', api_result:'接口观测 (api_result)', offline:'通道已断开 (offline)',
    peer_processed:'设备已处理 (peer_processed)', control_write_result:'控制下发结果 (control_write_result)', local_sampling:'本地采样调整 (local_sampling)',
    ack_written:'确认回执写出 (ack_written)', telemetry_written:'温度写出结果 (telemetry_written)', unconfirmed:'未收到确认 (unconfirmed)'};

  function selector(key, label) {
    const l = node('label', label), s = node('select');
    s.onchange = () => { q[key] = s.value; page = 1; draw(); };
    l.append(s); filterBar.append(l); selectors.set(key, s);
  }
  selector('device_id', '设备 / 网关');
  selector('endpoint', '观察端');
  selector('stage', '阶段 / 状态');
  const advancedFilters=node('details'),advancedBody=node('div',undefined,'toolbar'),conditions=node('p',undefined,'muted');advancedFilters.open=true;advancedFilters.append(node('summary','筛选选项（F07动态更新）'),advancedBody);
  for(const key of ['device_id','endpoint','stage'])advancedBody.append(selectors.get(key).parentElement);
  filterBar.append(advancedFilters);

  function refreshSelectors() {
    const values = {
      device_id: ['GW001', ...snapshot.devices.map(d => d.id), ...snapshot.events.map(e => e.device_id)],
      endpoint: snapshot.events.map(e => e.endpoint),
      stage: snapshot.events.map(e => e.stage)
    };
    for (const [key, select] of selectors) {
      const current = q[key] || '';
      const available = [...new Set(values[key].filter(v => typeof v === 'string' && v.length > 0))].sort();
      const first = node('option', '全部'); first.value = '';
      const options = [first];
      for (const value of available) {
        const option = node('option', labels[value] || value); option.value = value; options.push(option);
      }
      // Do not silently widen an export by clearing the user's vanished filter.
      if (current && !available.includes(current)) {
        const option = node('option', (labels[current] || current) + '（当前范围无匹配）');
        option.value = current; options.push(option);
      }
      select.replaceChildren(...options); select.value = current;
    }
  }

  const payload = node('input'); payload.type = 'checkbox';
  const pl = node('label', '明确允许导出模拟温度载荷（默认不含）'); pl.append(payload);
  const out = node('div'); out.id = 'export-results';
  const pager = node('div', undefined, 'toolbar');
  const prev = button('上一页', () => { page--; draw(); }, 'secondary');
  const next = button('下一页', () => { page++; draw(); }, 'secondary');
  prev.dataset.write = next.dataset.write = '0'; pager.append(prev, pages, next);
  const scrollAnchor = node('button', '', 'scroll-anchor-btn');
  scrollAnchor.type = 'button'; scrollAnchor.hidden = true;
  scrollAnchor.onclick = () => {
    const wrap = rows.querySelector?.('.table-wrap');
    if (wrap) {
      if (wrap.scrollTo) wrap.scrollTo({ top: wrap.scrollHeight, behavior: 'smooth' });
      else wrap.scrollTop = wrap.scrollHeight;
    }
    scrollAnchor.hidden = true; scrollAnchor.textContent = '';
  };
  c.append(node('p', '内存最近2000事件；公共文件最多20MiB。筛选分页针对固定事件范围，轮询不让记录跳页。', 'muted'),
    range, filterBar, conditions, rows, scrollAnchor, pager, pl);
  const exportMenu=node('details',undefined,'overflow'),exportItems=node('div',undefined,'menu');exportMenu.append(node('summary','导出记录 ⌄'),exportItems);filterBar.append(exportMenu);

  for (const format of ['json', 'csv', 'html']) {
    const exportButton = button('导出筛选记录 ' + format.toUpperCase(), async () => {
      out.replaceChildren();
      const fromEventId = (snapshot.events && snapshot.events.length > 0 && snapshot.events[0].event_id > 0) ? snapshot.events[0].event_id : (snapshot.cursor || 1);
      const activeFilters = Object.fromEntries(Object.entries(q).filter(([, v]) => typeof v === 'string' && v.trim() !== ''));
      const selection = {run_id:snapshot.run_id, from_event_id:fromEventId, to_event_id:snapshot.cursor, ...activeFilters};
      if(payload.checked && api.confirmPayload && !await api.confirmPayload())return;
      const r = await api('/exports', {format, include_payload:payload.checked, selection,
        ...(payload.checked ? {confirm:'include_simulated_payload'} : {})});
      api.validateExport?.(r,selection);
      const a = node('a', `下载 ${format} · ${r.event_count}事件 · ${r.bytes}字节`);
      a.href = r.url; a.download = '';
      out.append(a, node('p', 'SHA-256 ' + r.sha256, 'muted'),detail({selection:r.selection??selection,event_count:r.event_count,bytes:r.bytes,sha256:r.sha256}));
      api.notify?.('筛选文件已生成；请点击下载。生成文件不代表已保存到电脑。');
    }); writes.push(exportButton); exportItems.append(exportButton);
  }
  const updateButton = button('更新记录范围', async () => {
    const fresh = await api('/state');
    if (!fresh || fresh.run_id !== snapshot.run_id || !Array.isArray(fresh.events) || !Array.isArray(fresh.devices)) {
      throw Error('记录所属运行或数据结构已改变，请重新打开记录页面；未更新当前筛选范围。');
    }
    snapshot = fresh; page = 1; refreshSelectors(); draw();
    api.notify?.('记录范围已更新，保留当前筛选；后续轮询不会自动改变分页范围。');
  }, 'secondary'); updateButton.dataset.write='0';writes.push(updateButton); filterBar.replaceChildren(updateButton,...filterBar.children);c.append(out);
  all.append(c);

  function draw() {
    const matches = filterEvents(snapshot.events, q), p = pageEvents(matches, page);
    conditions.textContent='当前条件：'+(Object.entries(q).filter(([,v])=>v).map(([k,v])=>k+'='+v).join(' · ')||'全部');
    page = p.page; pages.textContent = `第 ${page}/${p.pages}页 · 匹配 ${matches.length}条`;
    prev.disabled = page === 1; next.disabled = page === p.pages;
    range.textContent = `run ${snapshot.run_id} · 事件 ${snapshot.events[0]?.event_id ?? 0}～${snapshot.cursor} · 内存已省略早期 ${snapshot.events_omitted}条（旧公共文件保留）；导出当前筛选全部匹配条目。`;
    const tbl = table(['时间','观察端 / 设备','阶段','详情'], p.rows.map(e => [
      new Date(e.at).toLocaleTimeString(), e.endpoint + ' / ' + (e.device_id || '本机'), badge(e.stage), detail(e)
    ]));
    rows.replaceChildren(tbl);
    if (!p.rows.length) rows.append(node('p', '该条件下没有记录。', 'muted'));
    scrollAnchor.hidden = true; scrollAnchor.textContent = '';
    if (typeof tbl.addEventListener === 'function') {
      tbl.addEventListener('scroll', () => {
        const scrolledUp = tbl.scrollTop > 30 && (tbl.scrollHeight - tbl.scrollTop - tbl.clientHeight > 40);
        if (scrolledUp) {
          scrollAnchor.textContent = '⬇️ 回到底部';
          scrollAnchor.hidden = false;
        } else {
          scrollAnchor.hidden = true;
          scrollAnchor.textContent = '';
        }
      });
    }
  }
  all.refresh = () => {if(api.store)for(const b of writes){b.guard=()=>api.store.authValid()?'':'本机授权不可用';b.disabled=!!b.dataset.busy||!!b.guard();}}; // Preserve intentional fixed-range pagination.
  lastView = {api, run: initial.run_id, view: all, restore() {
    // Same presentation guard as a freshly mounted page. Successful authorized
    // polling clears unavailable; a busy request stays disabled. API permission,
    // CSRF and Host/Origin are still enforced by the unchanged server/client.
    if (document.body.dataset.unavailable !== '1') {
      for (const b of writes) if (!b.dataset.busy) b.disabled = false;
    }
    draw();
  }};
  refreshSelectors(); draw(); return all;
}
