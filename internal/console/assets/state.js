// Terminal failures and cancelled controls are separate from pending work.
export function pendingControls(operations){
 return operations.filter(o=>o.kind==='control'&&['queued','writing','written','unconfirmed'].includes(o.status)).length;
}
