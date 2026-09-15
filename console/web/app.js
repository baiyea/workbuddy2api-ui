const $ = (id) => document.getElementById(id);
let csrf = '', modelList = [], accounts = [], history = [], conversation = newConversation(), activeRequest, flowID, flowTimer;
let page = 'overview', sessionGeneration = 0;
function newConversation() { return `web-${Date.now()}-${Math.random().toString(36).slice(2)}`; }
function notice(text = '') { $('notice').textContent = text; $('notice').hidden = !text; }
async function api(path, data, signal) {
 const response = await fetch('/admin/' + path, {method:data === undefined ? 'GET' : 'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:data === undefined ? undefined : JSON.stringify(data),signal});
 if (response.status === 401 && path !== 'login') { signedOut(); throw new Error('管理会话已过期，请重新登录'); }
 return response;
}
async function jsonAPI(path, data) {
 const generation = sessionGeneration;
 const response = await api(path, data); let result;
 try { result = await response.json(); } catch { throw new Error(`服务返回了非 JSON 响应（${response.status}）`); }
 if (path !== 'logout' && generation !== sessionGeneration) throw new Error('管理会话已退出，请重新登录');
 if (!response.ok) throw new Error(typeof result.error === 'string' ? result.error : result.error?.message || `请求失败（${response.status}）`);
 return result;
}
function signedOut() {
 sessionGeneration++;
 csrf = ''; activeRequest?.abort(); clearTimeout(flowTimer); flowID = undefined; history = []; conversation = newConversation();
 $('messages').replaceChildren(); $('api-key').value = ''; $('api-key').type = 'password'; $('admin-key').value = ''; $('console-view').hidden = true; $('login-view').hidden = false;
}
async function signedIn(session) {
 csrf = session.csrf; $('admin-key').value = ''; $('login-view').hidden = true; $('console-view').hidden = false;
 $('realm').querySelector('[value="global"]').disabled = !session.global_enabled;
 await refreshStatus(); await refreshModels();
}
document.querySelectorAll('[data-view]').forEach(button => button.addEventListener('click', () => {
 page = button.dataset.view;
 document.querySelectorAll('[data-page]').forEach(el => el.hidden = el.dataset.page !== page);
 document.querySelectorAll('.nav').forEach(el => el.classList.toggle('active', el.dataset.view === page));
 $('breadcrumb-name').textContent = {overview:'运行概览',accounts:'账号管理',chat:'对话测试',access:'API 接入'}[page];
 if (page !== 'access') { $('api-key').value = ''; $('api-key').type = 'password'; }
}));
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
  const previous=$('model').value; modelList=(await jsonAPI('models')).data||[]; $('model').replaceChildren();
  for (const model of modelList) { const option=new Option(model.id,model.id);$('model').append(option); }
  if (modelList.some(m=>m.id===previous)) $('model').value=previous;
  else {const available=modelList.find(m=>accounts.some(a=>m.id.startsWith(`${a.realm||'cn'}:`)));if(available)$('model').value=available.id;}
  updateEfforts();
 } catch(e) { notice(e.message); }
}
function updateModelHint() { const realm=$('model').value.startsWith('global:')?'global':'cn'; $('model-hint').textContent=accounts.some(a=>(a.realm||'cn')===realm&&!a.disabled)?'模型列表可能包含静态候选；是否可调用以实际回答为准。':'当前没有此版本的已启用账号，请先添加对应账号。'; }
function updateEfforts() { const model=modelList.find(m=>m.id===$('model').value);$('effort').replaceChildren(new Option('默认',''));for(const e of model?.reasoning_supported_efforts||[])$('effort').append(new Option(e,e));$('effort').disabled=!model?.reasoning_supported_efforts?.length;updateModelHint(); }
$('model').addEventListener('change',updateEfforts);
$('refresh').addEventListener('click',async()=>{await refreshStatus();await refreshModels();});
setInterval(()=>{if(csrf&&!document.hidden)refreshStatus();},15000);

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
$('clear-chat').addEventListener('click',()=>{if(activeRequest)return;clearChat();});
$('stop-chat').addEventListener('click',()=>activeRequest?.abort());
$('chat-form').addEventListener('submit',async event=>{
 event.preventDefault();if(activeRequest)return;const text=$('prompt').value.trim();if(!text)return;
 notice('');message('user',text);$('prompt').value='';const answer=message('assistant','正在等待模型…');
 const controller=new AbortController();activeRequest=controller;$('send-chat').disabled=true;$('stop-chat').hidden=false;$('clear-chat').disabled=true;
 let content='',reasoning='',details,reasonText,usage,finished=false;const tools=new Map();const outgoing=[...history,{role:'user',content:text}];
 try{
  const body={model:$('model').value,messages:outgoing,stream:true,conversationId:conversation};if($('effort').value)body.reasoning_effort=$('effort').value;
  const response=await api('chat',body,controller.signal);
  if(!response.ok){const err=await response.json();throw new Error(err.error?.message||err.error||`HTTP ${response.status}`);}
  const reader=response.body.getReader(),decoder=new TextDecoder();let buffer='';
  const processFrame=frame=>{
   const payload=frame.split('\n').filter(l=>l.startsWith('data:')).map(l=>l.slice(5).trimStart()).join('\n');if(!payload)return;
   if(payload==='[DONE]'){finished=true;return;}
   const part=JSON.parse(payload);if(part.error)throw new Error(part.error.message||'上游流式响应发生错误');
   const delta=part.choices?.[0]?.delta||{};content+=delta.content||'';
   if(delta.reasoning_content){reasoning+=delta.reasoning_content;if(!details){details=document.createElement('details');const summary=document.createElement('summary');summary.textContent='查看推理内容';reasonText=document.createElement('pre');details.append(summary,reasonText);answer.el.insertBefore(details,answer.content);}reasonText.textContent=reasoning;}
   for(const call of delta.tool_calls||[]){const prev=tools.get(call.index)||{name:'',arguments:''};prev.name+=call.function?.name||'';prev.arguments+=call.function?.arguments||'';tools.set(call.index,prev);}
   const toolText=tools.size?'\n\n工具调用（仅展示，不执行）：\n'+[...tools.values()].map(t=>`${t.name}\n${t.arguments}`).join('\n'):'';
   answer.content.textContent=content+toolText||'模型正在思考…';
   if(part.usage)usage=part.usage;
   $('messages').scrollTop=$('messages').scrollHeight;
  };
  while(true){const{value,done}=await reader.read();buffer+=done?decoder.decode():decoder.decode(value,{stream:true});buffer=buffer.replace(/\r\n/g,'\n');let split;while((split=buffer.indexOf('\n\n'))>=0){processFrame(buffer.slice(0,split));buffer=buffer.slice(split+2);}if(done){if(buffer.trim())processFrame(buffer);break;}}
  if(!finished)throw new Error('响应提前中断，可重新发送问题');
  if(!content&&!tools.size)answer.content.textContent='模型未返回文本内容。';
  history=[...outgoing,{role:'assistant',content:content||'[本轮返回了工具调用，控制台未执行工具]'}];
  $('usage').textContent=usage?`输入 ${usage.prompt_tokens??'—'} · 输出 ${usage.completion_tokens??'—'} · 总计 ${usage.total_tokens??'—'} tokens`:'上游未返回用量';
 }catch(e){const msg=e.name==='AbortError'?'已停止生成':e.message;answer.content.textContent=(content?content+'\n\n':'')+msg;$('usage').textContent=msg;}
 finally{controller.abort();activeRequest=undefined;$('send-chat').disabled=false;$('stop-chat').hidden=true;$('clear-chat').disabled=false;refreshStatus();}
});
$('base-url').value=location.origin+'/v1';
$('api-example').textContent=`curl ${location.origin}/v1/chat/completions \\\n  -H "Authorization: Bearer <你的 API Key>" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"cn:glm-5.2","messages":[{"role":"user","content":"你好"}]}'`;
$('reveal-key').addEventListener('click',async()=>{try{$('api-key').value=(await jsonAPI('access',{})).api_key;$('api-key').type='text';}catch(e){notice(e.message);}});
$('copy-key').addEventListener('click',async()=>{try{if(!$('api-key').value)$('api-key').value=(await jsonAPI('access',{})).api_key;await navigator.clipboard.writeText($('api-key').value);notice('API Key 已复制');}catch{$('api-key').type='text';$('api-key').select();notice('浏览器不允许自动复制，请手动复制选中的密钥');}});
jsonAPI('session').then(signedIn).catch(()=>{});
