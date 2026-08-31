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
