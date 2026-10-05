export function batchMessage(result){
 if(!Array.isArray(result?.results))return null;
 return `连接批次：尝试 ${result.attempted}，准入 ${result.accepted}，拒绝 ${result.rejected}。${result.results.map(r=>`${r.device_id} ${r.status}${r.error?'：'+r.error:''}`).join('；')}。准入不代表握手完成，请查看在线状态。`;
}

// MinimalUI summary only; full per-device observations remain in the disclosure.
export function batchSummary(result) {
 const counts=['attempted','accepted','rejected'];
 if(!Array.isArray(result?.results)||counts.some(k=>!Number.isSafeInteger(result[k])||result[k]<0)||result.attempted!==result.results.length||result.accepted+result.rejected!==result.attempted)
  return '批量回执字段不一致，不能核验准入结果；请查看公开明细。';
 return `尝试 ${result.attempted} · 准入 ${result.accepted} · 拒绝 ${result.rejected}。准入不代表握手完成；当前在线数量另行观测。`;
}
