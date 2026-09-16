const $ = (id) => document.getElementById(id);
let csrf = '', modelList = [], accounts = [], history = [], conversation = newConversation(), activeRequest, flowID, flowTimer;
let page = 'overview', sessionGeneration = 0;
let protocol = 'openai';
let taskState = {items:[],active_run:null,latest_runs:[]}, taskHistory = [], taskBefore = null, taskStarting = false, taskPollTimer, taskRenderKey, detailController, detailGeneration = 0;
let selectedTaskRun, historyGeneration = 0, historyLoading = false, taskHistoryKey;
const taskIntents = new Map(), taskReads = new Set();
function newConversation() { return `web-${Date.now()}-${Math.random().toString(36).slice(2)}`; }
function notice(text = '') { $('notice').textContent = text; $('notice').hidden = !text; }
async function api(path, data, signal) {
 const generation = sessionGeneration;
 const response = await fetch('/admin/' + path, {method:data === undefined ? 'GET' : 'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:data === undefined ? undefined : JSON.stringify(data),signal});
 if (path !== 'logout' && generation !== sessionGeneration) throw new Error('管理会话已退出，请重新登录');
 if (response.status === 401 && path !== 'login' && path !== 'logout') { signedOut(); throw new Error('管理会话已过期，请重新登录'); }
 return response;
}
async function jsonAPI(path, data, signal) {
 const generation = sessionGeneration;
 const response = await api(path, data, signal); let result;
 try { result = await response.json(); } catch { throw new Error(`服务返回了非 JSON 响应（${response.status}）`); }
 if (path !== 'logout' && generation !== sessionGeneration) throw new Error('管理会话已退出，请重新登录');
 if (!response.ok) { const error=new Error(typeof result.error === 'string' ? result.error : result.error?.message || `请求失败（${response.status}）`);error.status=response.status;error.runID=result.run_id;throw error; }
 return result;
}
function signedOut() {
 sessionGeneration++;
 csrf = ''; activeRequest?.abort(); clearTimeout(flowTimer); flowID = undefined; history = []; conversation = newConversation(); stopTaskReads(); taskIntents.clear(); taskState={items:[],active_run:null,latest_runs:[]};taskHistory=[];taskBefore=null;taskStarting=false;taskRenderKey=undefined;
 $('messages').replaceChildren();$('task-list').replaceChildren();$('task-history-body').replaceChildren();$('task-detail').hidden=true;$('task-accounts').textContent='';$('task-log').textContent=''; $('api-key').value = ''; $('api-key').type = 'password'; $('admin-key').value = ''; $('console-view').hidden = true; $('login-view').hidden = false;
 protocol='openai';$('prompt').value='';$('max-tokens').value='1024';$('usage').textContent='用量将在上游返回后显示';renderAccess();
}
async function signedIn(session) {
 csrf = session.csrf; $('admin-key').value = ''; $('login-view').hidden = true; $('console-view').hidden = false;
 $('realm').querySelector('[value="global"]').disabled = !session.global_enabled;
 await refreshStatus(); await refreshModels();
}
function showPage(value) {
 if (page === 'tasks' && value !== 'tasks') stopTaskReads(true);
 page = value;
 document.querySelectorAll('[data-page]').forEach(el => el.hidden = el.dataset.page !== page);
 document.querySelectorAll('.nav').forEach(el => el.classList.toggle('active', el.dataset.view === page));
 $('breadcrumb-name').textContent = {overview:'运行概览',accounts:'账号管理',tasks:'自动任务',chat:'对话测试',access:'API 接入'}[page];
 if (page !== 'access') { $('api-key').value = ''; $('api-key').type = 'password'; }
 if (page === 'tasks') loadTaskPage();
}
document.querySelectorAll('[data-view]').forEach(button => button.addEventListener('click', () => showPage(button.dataset.view)));
$('login-form').addEventListener('submit', async event => {
 event.preventDefault(); const button = event.submitter; button.disabled = true; $('login-error').textContent = '';
 try { const session = await jsonAPI('login', {key:$('admin-key').value}); await signedIn(session); } catch(error) { $('login-error').textContent = error.message; } finally { button.disabled = false; }
});
$('logout').addEventListener('click', async () => {$('login-error').textContent='';const pending=jsonAPI('logout', {});signedOut();try {await pending;} catch(e){$('login-error').textContent=e.message;} });
function cell(text, small) { const td = document.createElement('td'); td.textContent = text; if (small) { const s=document.createElement('small');s.textContent=small;td.append(s); } return td; }
function renderAccounts(data) {
 accounts = data.accounts || [];
 $('count-total').textContent = data.total; $('count-healthy').textContent = data.healthy; $('count-limited').textContent = `${data.cooling} / ${data.disabled}`;
 $('count-flight').textContent = accounts.reduce((n,a) => n + a.in_flight, 0);
 $('service-state').textContent = data.total ? '网关运行中' : '运行中 · 等待添加账号';
 $('welcome-title').textContent = data.total ? '你的网关已连接账号' : '添加第一个账号';
 $('welcome-text').textContent = data.total ? '检查账号状态，或发出一个问题来验证模型当前的响应。' : '在浏览器中完成授权，网关会自动保存并加载账号。';
 $('accounts-body').replaceChildren(); $('accounts-empty').hidden = accounts.length > 0;
 for (const a of accounts) {
  const tr=document.createElement('tr');const state=a.disabled?'已禁用':a.cooling?'冷却中':'可用';
  const status=cell('');const badge=document.createElement('span');badge.className='badge'+(state==='可用'?'':' warn');badge.textContent=state;status.append(badge);
  const limits=(a.rate_limited_models||[]).map(m => `${m.model} 至 ${new Date(m.until).toLocaleString()}`).join('；');
  tr.append(cell(a.nickname||a.uid,`${a.realm === 'global'?'国际版':'国内版'} · ${a.uid}`),status,cell(a.credits_known ? String(a.credits) : a.credits>0 ? `${a.credits}（历史）` : '待确认'),cell(`${a.success_count||0} / ${a.err_total||0}`),cell(String(a.in_flight)),cell(limits||a.disabled_reason||a.reason||'—', a.cool_remaining_sec ? `约 ${Math.ceil(a.cool_remaining_sec/60)} 分钟后恢复` : ''));
  $('accounts-body').append(tr);
 }
 updateModelHint();
}
async function refreshStatus() { try { renderAccounts(await jsonAPI('status')); } catch(e) { notice(e.message); } }
async function refreshModels() {
 try {
  const models=(await jsonAPI('models')).data||[];if(activeRequest)return;
  const previous=$('model').value,previousAccess=$('access-model').value;modelList=models;$('model').replaceChildren();$('access-model').replaceChildren();
  for (const model of modelList) { $('model').append(new Option(model.id,model.id));$('access-model').append(new Option(model.id,model.id)); }
  if (modelList.some(m=>m.id===previous)) $('model').value=previous;
  else {const available=modelList.find(m=>accounts.some(a=>m.id.startsWith(`${a.realm||'cn'}:`)));if(available)$('model').value=available.id;}
  $('access-model').value=modelList.some(m=>m.id===previousAccess)?previousAccess:$('model').value;
  updateEfforts();renderAccess();
 } catch(e) { notice(e.message); }
}
function updateModelHint() { const realm=$('model').value.startsWith('global:')?'global':'cn'; $('model-hint').textContent=accounts.some(a=>(a.realm||'cn')===realm&&!a.disabled)?'模型列表可能包含静态候选；是否可调用以实际回答为准。':'当前没有此版本的已启用账号，请先添加对应账号。'; }
function updateEfforts() { const model=modelList.find(m=>m.id===$('model').value),previous=$('effort').value;$('effort').replaceChildren(new Option('默认',''));for(const e of model?.reasoning_supported_efforts||[])$('effort').append(new Option(e,e));$('effort').value=model?.reasoning_supported_efforts?.includes(previous)?previous:'';$('effort').disabled=!!activeRequest||protocol==='anthropic'||!model?.reasoning_supported_efforts?.length;updateModelHint(); }
$('model').addEventListener('change',updateEfforts);
$('refresh').addEventListener('click',async()=>{await refreshStatus();await refreshModels();});
setInterval(()=>{if(csrf&&!document.hidden)refreshStatus();},15000);

const taskNames={checkin:'签到',travel:'猫猫旅行',activity:'活跃上报',keepalive:'Token 保活',school:'开学季',cat:'夜猫子'};
const runStatuses={running:'运行中',success:'成功',partial_failure:'部分失败',failed:'失败',skipped:'已跳过',interrupted:'已中断（结果未确认）',unknown:'结果未确认'};
function formatTaskReward(value){return value===null||value===undefined?'未确认':String(value);}
function formatTaskAvailability(task){return task.enabled===false?'已禁用':task.next_at?'已启用':'暂不可用 / 未知';}
function formatRunStatus(status){return runStatuses[status]||'结果未确认';}
function formatTaskTime(value){if(!value)return '未知';const date=new Date(value);return Number.isNaN(date.getTime())?'未知':date.toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false});}
function createTaskRequestID(){
 if(typeof crypto.randomUUID==='function')return crypto.randomUUID();
 const bytes=new Uint8Array(16);crypto.getRandomValues(bytes);return Array.from(bytes,byte=>byte.toString(16).padStart(2,'0')).join('');
}
function cancelTaskDetail(hide=false){detailGeneration++;detailController?.abort();detailController=undefined;if(hide){selectedTaskRun=undefined;$('task-detail').hidden=true;}}
function stopTaskReads(hideDetail=false){clearTimeout(taskPollTimer);taskPollTimer=undefined;historyGeneration++;historyLoading=false;for(const controller of taskReads)controller.abort();taskReads.clear();cancelTaskDetail(hideDetail);}
async function taskJSON(path){
 const controller=new AbortController();taskReads.add(controller);
 try{return await jsonAPI(path,undefined,controller.signal);}finally{taskReads.delete(controller);}
}
function taskPageVisible(){return page==='tasks'&&csrf&&!document.hidden;}
function scheduleTaskPoll(){
 clearTimeout(taskPollTimer);taskPollTimer=undefined;
 if(taskPageVisible())taskPollTimer=setTimeout(pollTaskRun,2000);
}
function scheduleTaskRetry(){clearTimeout(taskPollTimer);taskPollTimer=taskPageVisible()?setTimeout(loadTaskState,2000):undefined;}
async function loadTaskState(schedule=true){
 if(!taskPageVisible())return;
 try{taskState=await taskJSON('tasks');renderTasks();$('task-live').textContent=taskState.active_run?'后台任务正在运行，页面将自动刷新。':'任务状态已刷新。';if(schedule)scheduleTaskPoll();return true;}
 catch(error){if(error.name!=='AbortError'){$('task-live').textContent=error.message;notice(error.message);if(schedule)scheduleTaskRetry();}}
}
function taskRunKey(){return JSON.stringify([taskState.active_run,taskState.latest_runs]);}
async function loadTaskHistory(reset=false,refresh=false){
 if(!taskPageVisible())return;
 if(reset){historyGeneration++;historyLoading=false;}
 if(historyLoading||(!reset&&!refresh&&!taskBefore))return;
 const generation=historyGeneration,key=taskRunKey();historyLoading=true;$('task-more').disabled=true;
 const path='task-runs?limit=20'+(!reset&&!refresh&&taskBefore?'&before='+encodeURIComponent(taskBefore):'');
 try{
  const result=await taskJSON(path);if(generation!==historyGeneration||!taskPageVisible())return;
  if(refresh){const ids=new Set(result.items.map(run=>run.id));if(!taskHistory.length)taskBefore=result.next_before;taskHistory=[...result.items,...taskHistory.filter(run=>!ids.has(run.id))];}
  else{taskHistory=reset?result.items:[...taskHistory,...result.items];taskBefore=result.next_before;}
  if(reset||refresh)taskHistoryKey=key;renderTaskHistory();
 }catch(error){if(generation===historyGeneration&&error.name!=='AbortError')notice(error.message);}
 finally{if(generation===historyGeneration){historyLoading=false;$('task-more').disabled=false;}}
}
async function loadTaskPage(){
 stopTaskReads();taskHistory=[];taskBefore=null;renderTaskHistory();await Promise.all([loadTaskState(),loadTaskHistory(true)]);
}
function renderTasks(){
 const key=JSON.stringify([taskState,taskStarting,[...taskIntents.keys()]]);if(key===taskRenderKey)return;taskRenderKey=key;
 const list=$('task-list');list.replaceChildren();const active=taskState.active_run;
 for(const task of taskState.items||[]){
  const card=document.createElement('article');card.className='panel task-card';
  const heading=document.createElement('div');heading.className='task-card-heading';const title=document.createElement('h3');title.textContent=taskNames[task.id]||task.id;const badge=document.createElement('span');badge.className='badge'+(task.enabled===false?' warn':'');badge.textContent=formatTaskAvailability(task);heading.append(title,badge);
  const hours=document.createElement('p');hours.className='muted small';hours.textContent=`配置时间：${(task.hours||[]).map(hour=>String(hour).padStart(2,'0')+':00').join('、')||'未知'} · ${task.timezone==='Asia/Shanghai'?'北京时间':task.timezone||'时区未知'}`;
  const next=document.createElement('p');next.className='small';next.textContent=task.enabled===false?'下次执行：已禁用':`下次执行：${task.next_at?formatTaskTime(task.next_at):'暂不可用 / 未知'}`;
  const latest=(taskState.latest_runs||[]).find(run=>run.task_id===task.id);const summary=document.createElement('p');summary.className='muted small';summary.textContent=active?.task_id===task.id?`当前：${formatRunStatus(active.status)}`:latest?`最近：${formatRunStatus(latest.status)} · ${formatTaskTime(latest.started_at)}`:'最近：暂无记录';
  const actions=document.createElement('div');actions.className='task-actions';const run=document.createElement('button');run.className='primary';run.textContent=active?.task_id===task.id?'运行中':taskIntents.has(task.id)?'重试同一操作':'立即执行';run.disabled=task.enabled===false||taskStarting||!!active;run.addEventListener('click',()=>triggerTask(task.id));actions.append(run);
  if(latest){const detail=document.createElement('button');detail.className='secondary';detail.textContent='查看记录';detail.addEventListener('click',()=>loadTaskDetail(latest.id));actions.append(detail);}
  card.append(heading,hours,next,summary,actions);list.append(card);
 }
}
async function triggerTask(taskID){
 const task=(taskState.items||[]).find(item=>item.id===taskID);if(taskStarting||taskState.active_run||!task||task.enabled===false)return;
 const generation=sessionGeneration;taskStarting=true;const requestID=taskIntents.get(taskID)||createTaskRequestID();taskIntents.set(taskID,requestID);renderTasks();notice('');
 try{
  const run=await jsonAPI('tasks/'+taskID+'/runs',{request_id:requestID});if(generation!==sessionGeneration)return;taskIntents.delete(taskID);taskState.active_run=run;renderTaskDetail(run);await Promise.all([loadTaskState(),loadTaskHistory(true)]);
 }catch(error){if(generation!==sessionGeneration)return;if(error.status===409&&error.runID){taskIntents.delete(taskID);await loadTaskDetail(error.runID);await loadTaskState();}else notice(error.message+'；再次点击会沿用同一请求标识。');}
 finally{if(generation===sessionGeneration){taskStarting=false;renderTasks();}}
}
async function pollTaskRun(){
 if(!taskPageVisible())return;
 clearTimeout(taskPollTimer);taskPollTimer=undefined;
 try{
  if(!await loadTaskState(false))return;
  if(selectedTaskRun&&!$('task-detail').hidden)await loadTaskDetail(selectedTaskRun,false);
  if(taskHistoryKey!==taskRunKey())await loadTaskHistory(false,true);
 }finally{scheduleTaskPoll();}
}
function appendHistoryCell(row,text){const td=document.createElement('td');td.textContent=text;row.append(td);}
function renderTaskHistory(){
 const body=$('task-history-body');body.replaceChildren();$('task-history-empty').hidden=taskHistory.length>0;$('task-more').hidden=!taskBefore;
 for(const run of taskHistory){const row=document.createElement('tr');appendHistoryCell(row,taskNames[run.task_id]||run.task_id);appendHistoryCell(row,formatRunStatus(run.status));appendHistoryCell(row,formatTaskTime(run.started_at));appendHistoryCell(row,run.source==='scheduled'?'计划':'手动');const action=document.createElement('td');const button=document.createElement('button');button.className='quiet';button.textContent='查看详情';button.addEventListener('click',()=>loadTaskDetail(run.id));action.append(button);row.append(action);body.append(row);}
}
function formatBalance(balance){return balance?`余额 ${balance.value}（观测于 ${formatTaskTime(balance.observed_at)}）`:'余额未观测';}
function renderTaskDetail(run,scroll=true){
 selectedTaskRun=run.id;
 $('task-detail').hidden=false;$('task-detail-title').textContent=`${taskNames[run.task_id]||run.task_id} · ${formatRunStatus(run.status)}`;
 const finish=run.status==='interrupted'?`观察时间：${formatTaskTime(run.finished_at)}（不代表真实业务结束）`:`结束时间：${formatTaskTime(run.finished_at)}`;
 $('task-detail-meta').textContent=`开始时间：${formatTaskTime(run.started_at)} · ${finish} · 耗时：${run.duration_ms===null||run.duration_ms===undefined?'未知':run.duration_ms+' ms'}`;
 $('task-accounts').textContent=(run.accounts||[]).map(account=>`${account.uid} · ${formatRunStatus(account.status)} · ${account.detail||'无补充说明'}\n${formatBalance(account.before)} → ${formatBalance(account.after)} · 已确认奖励：${formatTaskReward(account.reward)}`).join('\n\n')||'没有账号结果。';
 $('task-log').textContent=run.log||'没有日志摘要。';$('task-log-truncated').hidden=!run.log_truncated;if(scroll)$('task-detail').scrollIntoView({block:'nearest'});
}
async function loadTaskDetail(id,scroll=true){
 if(!scroll&&(detailController||selectedTaskRun!==id||$('task-detail').hidden))return;
 selectedTaskRun=id;
 cancelTaskDetail();const generation=detailGeneration;const controller=new AbortController();detailController=controller;taskReads.add(controller);
 try{const run=await jsonAPI('task-runs/'+encodeURIComponent(id),undefined,controller.signal);if(generation===detailGeneration&&selectedTaskRun===id&&taskPageVisible())renderTaskDetail(run,scroll);}
 catch(error){if(generation===detailGeneration&&error.name!=='AbortError')notice(error.message);}
 finally{taskReads.delete(controller);if(detailController===controller)detailController=undefined;}
}
$('task-refresh').addEventListener('click',loadTaskPage);
$('task-more').addEventListener('click',()=>loadTaskHistory(false));
$('task-detail-close').addEventListener('click',()=>cancelTaskDetail(true));
document.addEventListener?.('visibilitychange',()=>{if(document.hidden)stopTaskReads();else if(page==='tasks')pollTaskRun();});

$('add-account').addEventListener('submit',async event=>{
 event.preventDefault();clearTimeout(flowTimer);const button=event.submitter;button.disabled=true;notice('');
 const popup=window.open('about:blank','_blank');if(popup)popup.opener=null;
 try {
  const flow=await jsonAPI('oauth',{realm:$('realm').value});flowID=flow.id;renderFlow(flow);
  if(popup)popup.location=flow.auth_url;pollFlow();
 } catch(e){if(popup)popup.close();notice(e.message);}finally{button.disabled=false;}
});
function renderFlow(flow){
 $('flow').hidden=false;$('flow-label').textContent={waiting:'等待授权',retry:'等待重试',needs_region:'需要补充地区',complete:'授权完成',failed:'授权失败'}[flow.status]||flow.status;
 $('flow-message').textContent=flow.message;$('auth-link').href=flow.auth_url;$('auth-link').hidden=flow.status==='complete';
 $('region-form').hidden=flow.status!=='needs_region';$('flow-retry').hidden=flow.status!=='retry';
 if(flow.countries){$('region').replaceChildren(new Option('请选择你的注册地区',''));for(const c of flow.countries)$('region').append(new Option(c.Name||c.EnName,c.IOS2));}
}
async function pollFlow(){
 const id=flowID;if(!id||!csrf)return;
 try{const flow=await jsonAPI(`oauth/${id}/poll`,{});if(id!==flowID)return;renderFlow(flow);
  if(flow.status==='complete'){await refreshStatus();await refreshModels();return;}
  if(flow.status==='failed'||flow.status==='needs_region')return;
  flowTimer=setTimeout(pollFlow,flow.status==='retry'?10000:2500);
 }catch(e){$('flow-message').textContent=e.message;$('flow-retry').hidden=false;}
}
$('flow-retry').addEventListener('click',()=>{clearTimeout(flowTimer);pollFlow();});
$('region-form').addEventListener('submit',async event=>{
 event.preventDefault();if(!$('region').value)return;const button=event.submitter;button.disabled=true;
 try{const flow=await jsonAPI(`oauth/${flowID}/region`,{region:$('region').value});renderFlow(flow);if(flow.status==='complete'){await refreshStatus();await refreshModels();}else if(flow.status==='retry'){flowTimer=setTimeout(pollFlow,10000);}}catch(e){notice(e.message);}finally{button.disabled=false;}
});

function message(role,text){
 $('messages').querySelector('.chat-empty')?.remove();const el=document.createElement('div');el.className='message '+role;
 const label=document.createElement('span');label.className='role';label.textContent=role==='user'?'你':'ASSISTANT';const content=document.createElement('div');content.textContent=text;
 el.append(label,content);$('messages').append(el);el.scrollIntoView({block:'nearest'});return{el,content};
}
function clearChat(){history=[];conversation=newConversation();$('messages').replaceChildren();$('usage').textContent='用量将在上游返回后显示';}
function setProtocol(value){
 if(activeRequest||!['openai','anthropic'].includes(value)||value===protocol)return;
 protocol=value;clearChat();updateEfforts();renderAccess();
}
function renderAccess(){
 const anthropic=protocol==='anthropic',model=$('access-model').value;
 document.querySelectorAll('[data-protocol]').forEach(button=>{button.setAttribute('aria-pressed',String(button.dataset.protocol===protocol));button.disabled=!!activeRequest;});
 $('effort').hidden=$('effort-label').hidden=anthropic;
 $('max-tokens').hidden=$('max-tokens-label').hidden=!anthropic;
 $('base-url').value=location.origin+(anthropic?'':'/v1');
 $('api-endpoint').value=location.origin+(anthropic?'/v1/messages':'/v1/chat/completions');
 $('api-auth').textContent=anthropic?'x-api-key · anthropic-version: 2023-06-01':'Authorization: Bearer <API Key>';
 $('protocol-support').textContent=anthropic?'Anthropic · Beta 测试 · 文本兼容':'OpenAI · Chat Completions';
 $('protocol-note').textContent=anthropic?'Base URL 使用站点根地址，SDK 会追加 /v1/messages。支持文本、多轮和流式回答；暂不支持图片、工具、thinking，也不承诺 Claude Code 兼容。':'Base URL 包含 /v1，使用兼容 Chat Completions 的客户端连接。';
 const available=modelList.some(item=>item.id===model);
 $('copy-example').disabled=!available||!!activeRequest;$('go-chat').disabled=!available||!!activeRequest;
 if(!available){$('api-example').textContent='暂无可选模型，请先刷新模型列表。';return;}
 const body={model,...(anthropic?{max_tokens:1024}:{}),messages:[{role:'user',content:'你好'}],stream:true};
 const auth=anthropic?'  -H "x-api-key: <你的 API Key>" \\\n  -H "anthropic-version: 2023-06-01"':'  -H "Authorization: Bearer <你的 API Key>"';
 const json=JSON.stringify(body,null,2).replace(/'/g,"'\\''");
 $('api-example').textContent=`curl ${$('api-endpoint').value} \\\n${auth} \\\n  -H "Content-Type: application/json" \\\n  -d '${json}'`;
}
document.querySelectorAll('[data-protocol]').forEach(button=>button.addEventListener('click',()=>setProtocol(button.dataset.protocol)));
$('access-model').addEventListener('change',renderAccess);
$('copy-example').addEventListener('click',async()=>{if($('copy-example').disabled)return;try{await navigator.clipboard.writeText($('api-example').textContent);notice('调用示例已复制，请替换 API Key 占位符');}catch{notice('浏览器不允许自动复制，请手动复制下方示例');}});
$('go-chat').addEventListener('click',()=>{if(activeRequest||$('go-chat').disabled)return;$('model').value=$('access-model').value;updateEfforts();showPage('chat');});
function chatControls(){
 for(const id of ['model','access-model','clear-chat','send-chat','max-tokens'])$(id).disabled=!!activeRequest;
 $('stop-chat').hidden=!activeRequest;
 renderAccess();
}
$('clear-chat').addEventListener('click',()=>{if(activeRequest)return;clearChat();});
$('stop-chat').addEventListener('click',()=>activeRequest?.abort());
$('chat-form').addEventListener('submit',async event=>{
 event.preventDefault();if(activeRequest)return;const text=$('prompt').value.trim();if(!text)return;
 const requestProtocol=protocol,generation=sessionGeneration;
 if(requestProtocol==='anthropic'&&(!Number.isSafeInteger(Number($('max-tokens').value))||Number($('max-tokens').value)<=0)){notice('最大输出 tokens 必须是安全的正整数');return;}
 notice('');message('user',text);$('prompt').value='';const answer=message('assistant','正在等待模型…');
 const controller=new AbortController();activeRequest=controller;chatControls();$('effort').disabled=true;
 let content='',reasoning='',details,reasonText,usage,finished=false;const tools=new Map();const outgoing=[...history,{role:'user',content:text}];
 try{
  const body={model:$('model').value,messages:outgoing,stream:true,conversationId:conversation};if($('effort').value)body.reasoning_effort=$('effort').value;
  const anthropicBody={model:$('model').value,max_tokens:Number($('max-tokens').value),messages:outgoing,stream:true};
  const response=requestProtocol==='anthropic'?await api('messages',{conversation_id:conversation,request:anthropicBody},controller.signal):await api('chat',body,controller.signal);
  if(!response.ok){const err=await response.json();throw new Error(err.error?.message||err.error||`HTTP ${response.status}`);}
  const reader=response.body.getReader(),decoder=new TextDecoder();let buffer='';
  const processFrame=frame=>{
   const payload=frame.split('\n').filter(l=>l.startsWith('data:')).map(l=>l.slice(5).trimStart()).join('\n');if(!payload)return;
   if(payload==='[DONE]'){if(requestProtocol==='openai')finished=true;return;}
   const part=JSON.parse(payload);if(part.error)throw new Error(part.error.message||'上游流式响应发生错误');
   if(requestProtocol==='anthropic'){
    if(part.type==='error')throw new Error('上游流式响应发生错误');
    if(part.type==='content_block_delta'&&part.delta?.type==='text_delta'){content+=part.delta.text||'';answer.content.textContent=content;}
    if(part.type==='message_delta'&&part.usage)usage=part.usage;
    if(part.type==='message_stop')finished=true;
    $('messages').scrollTop=$('messages').scrollHeight;return;
   }
   const delta=part.choices?.[0]?.delta||{};content+=delta.content||'';
   if(delta.reasoning_content){reasoning+=delta.reasoning_content;if(!details){details=document.createElement('details');const summary=document.createElement('summary');summary.textContent='查看推理内容';reasonText=document.createElement('pre');details.append(summary,reasonText);answer.el.insertBefore(details,answer.content);}reasonText.textContent=reasoning;}
   for(const call of delta.tool_calls||[]){const prev=tools.get(call.index)||{name:'',arguments:''};prev.name+=call.function?.name||'';prev.arguments+=call.function?.arguments||'';tools.set(call.index,prev);}
   const toolText=tools.size?'\n\n工具调用（仅展示，不执行）：\n'+[...tools.values()].map(t=>`${t.name}\n${t.arguments}`).join('\n'):'';
   answer.content.textContent=content+toolText||'模型正在思考…';
   if(part.usage)usage=part.usage;
   $('messages').scrollTop=$('messages').scrollHeight;
  };
  while(true){const{value,done}=await reader.read();if(controller.signal.aborted){const error=new Error('已停止生成');error.name='AbortError';throw error;}buffer+=done?decoder.decode():decoder.decode(value,{stream:true});buffer=buffer.replace(/\r\n/g,'\n');let split;while((split=buffer.indexOf('\n\n'))>=0){processFrame(buffer.slice(0,split));buffer=buffer.slice(split+2);}if(done){if(buffer.trim())processFrame(buffer);break;}}
  if(!finished)throw new Error('响应提前中断，可重新发送问题');
  if(!content&&!tools.size)answer.content.textContent='模型未返回文本内容。';
  history=[...outgoing,{role:'assistant',content:content||(requestProtocol==='openai'?'[本轮返回了工具调用，控制台未执行工具]':'')}];
  $('usage').textContent=requestProtocol==='anthropic'?(usage?.input_tokens!=null||usage?.output_tokens!=null?`输入 ${usage.input_tokens??'—'} · 输出 ${usage.output_tokens??'—'} tokens`:'上游未返回用量'):(usage?`输入 ${usage.prompt_tokens??'—'} · 输出 ${usage.completion_tokens??'—'} · 总计 ${usage.total_tokens??'—'} tokens`:'上游未返回用量');
 }catch(e){if(generation===sessionGeneration){const msg=e.name==='AbortError'?'已停止生成':e.message;answer.content.textContent=(content?content+'\n\n':'')+msg;$('usage').textContent=msg;}}
 finally{controller.abort();activeRequest=undefined;chatControls();updateEfforts();if(generation===sessionGeneration)refreshStatus();}
});
renderAccess();
$('reveal-key').addEventListener('click',async()=>{try{$('api-key').value=(await jsonAPI('access',{})).api_key;$('api-key').type='text';}catch(e){notice(e.message);}});
$('copy-key').addEventListener('click',async()=>{const generation=sessionGeneration;try{if(!$('api-key').value)$('api-key').value=(await jsonAPI('access',{})).api_key;await navigator.clipboard.writeText($('api-key').value);if(generation===sessionGeneration)notice('API Key 已复制');}catch{if(generation!==sessionGeneration)return;$('api-key').type='text';$('api-key').select();notice('浏览器不允许自动复制，请手动复制选中的密钥');}});
jsonAPI('session').then(signedIn).catch(()=>{});
