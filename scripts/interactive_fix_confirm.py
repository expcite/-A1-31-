from pathlib import Path
root=Path(__file__).resolve().parents[1]
p=root/'internal/console/assets/keys.js';s=p.read_text(encoding='utf-8')
if "from'./confirm.js'" not in s:s="import{confirmAction}from'./confirm.js';\n"+s
old="window.prompt('目标 '+d.id+'，动作 '+label+'。将结束该设备当前会话；吊销/退役持久禁止重连。输入 '+expected+' 确认：')"
s=s.replace(old,'await confirmAction(d.id,label,expected)');p.write_text(s,encoding='utf-8')
p=root/'internal/console/assets/style.css';s=p.read_text(encoding='utf-8')
if '.confirm-form' not in s:s+='\ndialog{border:1px solid #bdceda;border-radius:12px;padding:25px;max-width:540px;color:#173048}dialog::backdrop{background:#102e4680}.confirm-form{display:block}.confirm-form button{margin-right:12px}.confirm-form input{width:100%}\n'
p.write_text(s,encoding='utf-8')
