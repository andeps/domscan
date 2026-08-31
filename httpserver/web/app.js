const form = document.querySelector('#search-form');
const button = document.querySelector('#submit-btn');
const stopButton = document.querySelector('#stop-btn');
const errorBox = document.querySelector('#form-error');
const tbody = document.querySelector('#results-body');
const progress = document.querySelector('#progress');
const searchNote = document.querySelector('#search-note');
const filter = document.querySelector('#status-filter');
const exportButton = document.querySelector('#export-btn');
const detailDialog = document.querySelector('#domain-dialog');
const availableDialog = document.querySelector('#available-dialog');
const availableList = document.querySelector('#available-list');
const detailFields = Object.fromEntries(['status','domain','label','tld','length','source','provider','expiration','duration','time','message'].map(name => [name, document.querySelector(`#detail-${name}`)]));
const counts = {
  checked: document.querySelector('#checked-count'),
  available: document.querySelector('#available-count'),
  registered: document.querySelector('#registered-count'),
  unknown: document.querySelector('#unknown-count')
};
let results = [];
let activeController = null;
let currentUser = JSON.parse(localStorage.getItem('domscan-user') || 'null');
let authToken = localStorage.getItem('domscan-token') || '';
let authMode = 'login';
const authDialog = document.querySelector('#auth-dialog');
const userDialog = document.querySelector('#user-dialog');
function refreshAccountUI() { document.querySelector('#login-open').hidden = !!currentUser; document.querySelector('#user-open').hidden = !currentUser; }
function openAuth(mode) { authMode = mode; document.querySelector('#auth-title').textContent = mode === 'login' ? '登录' : '注册'; document.querySelector('#auth-switch').textContent = mode === 'login' ? '没有账号？注册' : '已有账号？登录'; document.querySelector('#auth-error').textContent = ''; authDialog.showModal(); }
document.querySelector('#login-open').addEventListener('click', () => openAuth('login'));
document.querySelector('#register-open').addEventListener('click', () => openAuth('register'));
document.querySelector('#auth-close').addEventListener('click', () => authDialog.close());
document.querySelector('#auth-switch').addEventListener('click', () => openAuth(authMode === 'login' ? 'register' : 'login'));
document.querySelector('#auth-form').addEventListener('submit', async event => { event.preventDefault(); const error = document.querySelector('#auth-error'); error.textContent = ''; const payload = {email: document.querySelector('#auth-email').value, password: document.querySelector('#auth-password').value}; try { const response = await fetch(`/api/auth/${authMode}`, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(payload)}); const data = await response.json(); if (!response.ok) throw new Error(data.error || '操作失败'); currentUser = data.user; authToken = data.token || authToken; localStorage.setItem('domscan-user', JSON.stringify(currentUser)); localStorage.setItem('domscan-token', authToken); refreshAccountUI(); authDialog.close(); } catch (err) { error.textContent = err.message; } });
document.querySelector('#user-open').addEventListener('click', () => { document.querySelector('#user-email').textContent = currentUser?.email || ''; document.querySelector('#user-created').textContent = currentUser?.createdAt ? new Date(currentUser.createdAt).toLocaleString('zh-CN') : '—'; document.querySelector('#user-avatar').src = currentUser?.avatar || ''; userDialog.showModal(); });
async function profileRequest(path, body) { const response = await fetch(path, {method:'PUT', headers:{'Content-Type':'application/json','Authorization':`Bearer ${authToken}`}, body:JSON.stringify(body)}); const data = await response.json(); if (!response.ok) throw new Error(data.error || '操作失败'); return data; }
document.querySelector('#email-form').addEventListener('submit', async e => { e.preventDefault(); try { const data = await profileRequest('/api/auth/email', {email:currentUser.email,newEmail:document.querySelector('#new-email').value}); currentUser.email=data.email; localStorage.setItem('domscan-user',JSON.stringify(currentUser)); document.querySelector('#user-email').textContent=data.email; document.querySelector('#user-message').textContent='邮箱修改成功'; } catch(err) { document.querySelector('#user-message').textContent=err.message; } });
document.querySelector('#password-form').addEventListener('submit', async e => { e.preventDefault(); try { await profileRequest('/api/auth/password', {email:currentUser.email,password:document.querySelector('#new-password').value}); e.target.reset(); document.querySelector('#user-message').textContent='密码修改成功'; } catch(err) { document.querySelector('#user-message').textContent=err.message; } });
document.querySelector('#avatar-file').addEventListener('change', e => { const file=e.target.files[0]; if (!file || !currentUser) return; if(file.size>1.5*1024*1024){document.querySelector('#user-message').textContent='头像不能超过 1.5MB';return;} const reader=new FileReader(); reader.onload=async()=>{ try { const data=await profileRequest('/api/auth/avatar',{email:currentUser.email,avatar:reader.result}); currentUser.avatar=data.avatar; localStorage.setItem('domscan-user',JSON.stringify(currentUser)); document.querySelector('#user-avatar').src=data.avatar; document.querySelector('#user-message').textContent='头像更新成功'; } catch(err){document.querySelector('#user-message').textContent=err.message;} }; reader.readAsDataURL(file); });
document.querySelector('#user-close').addEventListener('click', () => userDialog.close());
document.querySelector('#logout-btn').addEventListener('click', () => { currentUser = null; authToken = ''; localStorage.removeItem('domscan-user'); localStorage.removeItem('domscan-token'); refreshAccountUI(); userDialog.close(); });
refreshAccountUI();
const quickResult = document.querySelector('#quick-result');
const tldCards = document.querySelector('#tld-cards');
document.querySelector('#quick-form').addEventListener('submit', async e => { e.preventDefault(); const value=document.querySelector('#quick-domain').value.trim().toLowerCase(); quickResult.textContent='查询中…'; try { const response=await fetch('/api/check',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({domains:[value],concurrency:1})}); if(!response.ok) throw new Error('域名格式无效'); const data=JSON.parse((await response.text()).split('\n')[0]); quickResult.textContent=`${statusText(data.status)} · 到期时间：${formatExpiration(data.expirationTime)}`; } catch(err) { quickResult.textContent=err.message; } });
document.querySelector('#quick-batch').addEventListener('click', () => document.querySelector('#search-form').scrollIntoView({behavior:'smooth'}));
function renderTLDs() { const base=document.querySelector('#quick-domain').value.trim().split('.')[0] || 'fhcode'; tldCards.innerHTML=''; ['com','net','org','io','ai','cn'].forEach(tld => { const card=document.createElement('button'); card.className='tld-card'; card.type='button'; card.innerHTML='<strong></strong><span>待检测</span>'; card.firstElementChild.textContent=`${base}.${tld}`; card.addEventListener('click',()=>{document.querySelector('#quick-domain').value=`${base}.${tld}`; document.querySelector('#quick-form').requestSubmit();}); tldCards.appendChild(card); }); }
renderTLDs(); document.querySelector('#quick-domain').addEventListener('input', renderTLDs);

function tokens(value) { return value.split(/[\s,，;；]+/).map(v => v.trim()).filter(Boolean); }
function options() {
  return {
    keywords: tokens(document.querySelector('#keywords').value),
    tlds: tokens(document.querySelector('#tlds').value),
    minLength: Number(document.querySelector('#min-length').value),
    maxLength: Number(document.querySelector('#max-length').value),
    digitMode: document.querySelector('#digit-mode').value,
    fuzzyMode: document.querySelector('#fuzzy-mode').value,
    hyphen: document.querySelector('#hyphen').checked,
    limit: Number(document.querySelector('#limit').value)
  };
}
async function jsonRequest(url, body) {
  const response = await fetch(url, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)});
  const payload = await response.json();
  if (!response.ok) throw new Error(payload.error || '请求失败');
  return payload;
}
function resetResults() {
  results = []; tbody.innerHTML = '';
  searchNote.textContent = '';
  Object.values(counts).forEach(node => node.textContent = '0');
  exportButton.disabled = true;
}
function updateCounts() {
  counts.checked.textContent = results.length;
  for (const status of ['available','registered','unknown']) counts[status].textContent = results.filter(r => r.status === status).length;
}
function showAvailableList() {
  availableList.innerHTML = '';
  const available = results.filter(r => r.status === 'available');
  if (!available.length) { availableList.textContent = '暂无可注册域名'; }
  available.forEach(result => {
    const item = document.createElement('button'); item.type = 'button'; item.className = 'available-item';
    item.innerHTML = '<strong></strong><span></span>'; item.firstElementChild.textContent = result.domain;
    item.lastElementChild.textContent = `${formatExpiration(result.expirationTime)} · ${result.cached ? '缓存' : '实时查询'}`;
    item.addEventListener('click', () => { availableDialog.close(); showDomainDetails(result); }); availableList.appendChild(item);
  });
  availableDialog.showModal();
}
function statusText(status) { return ({available:'可注册', registered:'已注册', unknown:'未知'})[status] || status; }
function formatExpiration(value) { if (!value) return '—'; const date = new Date(value); return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN'); }
function addResult(result) {
  result.checkedAt = new Date();
  results.push(result);
  const row = document.createElement('tr'); row.dataset.status = result.status;
  const labelLength = result.domain.split('.')[0].length;
  const displayStatus = result.cached ? `${statusText(result.status)} · 缓存` : statusText(result.status);
  const values = [result.domain, `${labelLength} 字符`, displayStatus, formatExpiration(result.expirationTime), `${result.durationMs} ms`];
  values.forEach((value, index) => {
    const cell = document.createElement('td'); cell.textContent = value;
    if (index === 0) cell.className = 'domain';
    if (index === 2) { cell.className = `badge ${result.status}`; cell.title = result.message || ''; }
    row.appendChild(cell);
  });
  tbody.appendChild(row); applyFilter(); updateCounts();
  if (result.status === 'available') {
    row.classList.add('clickable-result'); row.tabIndex = 0; row.title = '点击查看域名详情';
    row.addEventListener('click', () => showDomainDetails(result));
    row.addEventListener('keydown', event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); showDomainDetails(result); } });
  }
}
function showDomainDetails(result) {
  const parts = result.domain.split('.');
  detailFields.status.textContent = statusText(result.status);
  detailFields.domain.textContent = result.domain;
  detailFields.label.textContent = parts[0];
  detailFields.tld.textContent = parts.slice(1).join('.');
  detailFields.length.textContent = `${parts[0].length} 字符`;
  detailFields.source.textContent = result.cached ? 'Redis 缓存' : 'RDAP 实时查询';
  detailFields.provider.textContent = result.provider || (result.cached ? '历史缓存' : '未提供');
  detailFields.expiration.textContent = formatExpiration(result.expirationTime);
  detailFields.duration.textContent = `${result.durationMs} ms`;
  detailFields.time.textContent = result.checkedAt.toLocaleString('zh-CN');
  detailFields.message.textContent = result.message || '无附加说明';
  detailDialog.showModal();
}
document.querySelector('#detail-close').addEventListener('click', () => detailDialog.close());
document.querySelector('#available-stat').addEventListener('click', showAvailableList);
document.querySelector('#available-close').addEventListener('click', () => availableDialog.close());
availableDialog.addEventListener('click', event => { if (event.target === availableDialog) availableDialog.close(); });
detailDialog.addEventListener('click', event => { if (event.target === detailDialog) detailDialog.close(); });
document.querySelector('#detail-copy').addEventListener('click', async event => {
  await navigator.clipboard.writeText(detailFields.domain.textContent);
  event.currentTarget.firstElementChild.textContent = '已复制';
  setTimeout(() => { event.currentTarget.firstElementChild.textContent = '复制完整域名'; }, 1200);
});
async function streamSearch(searchOptions, signal) {
  const response = await fetch('/api/search', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({options:searchOptions, concurrency:8}), signal});
  if (!response.ok) { const body = await response.json(); throw new Error(body.error || '检测失败'); }
  const reader = response.body.getReader(); const decoder = new TextDecoder(); let buffer = '';
  while (true) {
    const {value, done} = await reader.read(); buffer += decoder.decode(value || new Uint8Array(), {stream:!done});
    const lines = buffer.split('\n'); buffer = lines.pop() || '';
    for (const line of lines) if (line.trim()) handleSearchEvent(JSON.parse(line));
    if (done) break;
  }
  if (buffer.trim()) handleSearchEvent(JSON.parse(buffer));
}
function handleSearchEvent(event) {
  if (event.event === 'summary') {
    searchNote.textContent = event.cachedSkipped ? `已跳过 ${event.cachedSkipped} 个缓存域名，继续检测了后续候选。` : `本次检测 ${event.fresh} 个新候选。`;
    if (event.notificationQueued) searchNote.textContent += ` 已为 ${event.notificationQueued} 个可注册域名启动后台邮箱通知。`;
    return;
  }
  addResult(event);
}
form.addEventListener('submit', async event => {
  event.preventDefault(); errorBox.textContent = ''; button.disabled = true; stopButton.hidden = false; button.firstElementChild.textContent = '正在检测…'; progress.hidden = false; resetResults();
  activeController = new AbortController();
  try {
    await streamSearch(options(), activeController.signal);
  } catch (error) {
    if (error.name === 'AbortError') searchNote.textContent = '检测已停止，已保留当前结果。';
    else errorBox.textContent = error.message;
  } finally { exportButton.disabled = results.length === 0; activeController = null; button.disabled = false; stopButton.hidden = true; button.firstElementChild.textContent = '生成并开始检测'; progress.hidden = true; }
});
stopButton.addEventListener('click', () => {
  if (activeController) { searchNote.textContent = '正在停止检测…'; activeController.abort(); }
});
function applyFilter() {
  const wanted = filter.value;
  tbody.querySelectorAll('tr[data-status]').forEach(row => row.classList.toggle('hidden-row', wanted !== 'all' && row.dataset.status !== wanted));
}
filter.addEventListener('change', applyFilter);
exportButton.addEventListener('click', () => {
  const rows = [['域名','状态','来源','到期时间','耗时(ms)','说明'], ...results.map(r => [r.domain,statusText(r.status),r.cached ? 'Redis 缓存' : 'RDAP 实时查询',formatExpiration(r.expirationTime),r.durationMs,r.message || ''])];
  const csv = '\ufeff' + rows.map(row => row.map(v => `"${String(v).replaceAll('"','""')}"`).join(',')).join('\n');
  const link = document.createElement('a'); link.href = URL.createObjectURL(new Blob([csv], {type:'text/csv;charset=utf-8'})); link.download = `domain-results-${new Date().toISOString().slice(0,10)}.csv`; link.click(); URL.revokeObjectURL(link.href);
});
