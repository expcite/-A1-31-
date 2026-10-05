export const routes=[['communication','工作台','01'],['devices','设备','02'],['keys','管理','03'],['lab','实验','04'],['logs','记录','05']];
export function routeFrom(hash){const id=String(hash).replace(/^#\/?/,'');return [...routes.map(r=>r[0]),'help'].includes(id)?id:'communication'}
