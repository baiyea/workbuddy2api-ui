// Run with: node --test console/web_test.cjs (no frontend dependencies).
const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const vm = require('node:vm');

test('malformed SSE cancels the live request and restores controls', async () => {
  const elements = new Map();
  function element() {
    return {value:'', hidden:false, disabled:false, textContent:'', handlers:{},
      addEventListener(name, fn) { this.handlers[name] = fn; },
      append(){}, replaceChildren(){}, scrollIntoView(){}, querySelector(){return null;}};
  }
  function get(id) {
    if (!elements.has(id)) elements.set(id, element());
    return elements.get(id);
  }
  let signal;
  const ctx = {
    document: {getElementById:get, querySelectorAll:()=>[], createElement:element},
    location:{origin:'http://console.test'}, AbortController, TextDecoder, Option:function(text,value){return {textContent:text,value};},
    setInterval(){}, clearTimeout(){},
    fetch:async (url, options) => {
      if (url !== '/admin/chat') throw new Error('not used');
      signal = options.signal;
      return {ok:true, status:200, body:{getReader:()=>({read:async()=>({done:false, value:new TextEncoder().encode('data: {invalid}\n\n')})})}};
    }
  };
  vm.runInNewContext(readFileSync(__dirname+'/web/app.js','utf8'), ctx);
  get('prompt').value = 'hello';
  get('model').value = 'cn:glm-5.2';
  await get('chat-form').handlers.submit({preventDefault(){}});
  assert.ok(signal.aborted, 'parser failure left upstream running');
  assert.equal(get('send-chat').disabled, false);
  assert.equal(get('stop-chat').hidden, true);
});

function logoutFixture(fetch) {
  const elements = new Map();
  const element = () => ({value:'', type:'password', hidden:false, textContent:'', children:[], handlers:{},
    addEventListener(name, fn){this.handlers[name]=fn;},
    replaceChildren(...children){this.children=children;}, select(){}});
  const get = id => {if(!elements.has(id))elements.set(id,element());return elements.get(id);};
  const controller = new AbortController();
  const ctx = vm.createContext({document:{getElementById:get,querySelectorAll:()=>[]},
    location:{origin:'http://console.test'},setInterval(){},clearTimeout(){},fetch,controller});
  vm.runInContext(readFileSync(__dirname+'/web/app.js','utf8'),ctx);
  vm.runInContext("csrf='active-csrf';history=[{role:'user',content:'private chat'}];activeRequest=controller;flowID='active-flow';",ctx);
  get('messages').children=['private chat'];get('api-key').value='revealed-api-key';get('api-key').type='text';
  get('admin-key').value='private-admin-key';get('login-view').hidden=true;get('console-view').hidden=false;
  return {get,ctx,controller};
}

test('task reward distinguishes unknown from confirmed zero', () => {
  const {ctx}=logoutFixture(()=>new Promise(()=>{}));
  assert.equal(vm.runInContext('formatTaskReward(null)',ctx),'未确认');
  assert.equal(vm.runInContext('formatTaskReward(undefined)',ctx),'未确认');
  assert.equal(vm.runInContext('formatTaskReward(0)',ctx),'0');
  assert.equal(vm.runInContext('formatTaskReward(5)',ctx),'5');
});

test('task request id uses secure random bytes when randomUUID is unavailable', () => {
  const {ctx}=logoutFixture(()=>new Promise(()=>{}));
  ctx.crypto={getRandomValues(bytes){for(let i=0;i<bytes.length;i++)bytes[i]=i;return bytes;}};
  assert.equal(vm.runInContext('createTaskRequestID()',ctx),'000102030405060708090a0b0c0d0e0f');
  ctx.crypto={randomUUID(){return '11111111-2222-4333-8444-555555555555';},getRandomValues(){throw new Error('fallback used');}};
  assert.equal(vm.runInContext('createTaskRequestID()',ctx),'11111111-2222-4333-8444-555555555555');
});

test('task truth and all run statuses are formatted without guessing', () => {
  const {ctx}=logoutFixture(()=>new Promise(()=>{}));
  assert.equal(vm.runInContext("formatTaskAvailability({enabled:false,next_at:null})",ctx),'已禁用');
  assert.equal(vm.runInContext("formatTaskAvailability({enabled:true,next_at:null})",ctx),'暂不可用 / 未知');
  for(const [status,label] of Object.entries({running:'运行中',success:'成功',partial_failure:'部分失败',failed:'失败',skipped:'已跳过',interrupted:'已中断（结果未确认）',unknown:'结果未确认'})) {
    ctx.testStatus=status;
    assert.equal(vm.runInContext('formatRunStatus(testStatus)',ctx),label,status);
  }
});

function taskFixture(fetch, cryptoImpl={randomUUID:()=> '11111111-2222-4333-8444-555555555555'}) {
  const elements=new Map();
  function element(tag='div') {
    return {tagName:tag.toUpperCase(),value:'',type:'',hidden:false,disabled:false,textContent:'',className:'',children:[],dataset:{},handlers:{},
      addEventListener(name,fn){this.handlers[name]=fn;},append(...children){this.children.push(...children);},appendChild(child){this.children.push(child);return child;},
      replaceChildren(...children){this.children=children;},setAttribute(name,value){this[name]=value;},insertBefore(child){this.children.unshift(child);},querySelector(){return null;},scrollIntoView(){},select(){}};
  }
  const get=id=>{if(!elements.has(id))elements.set(id,element());return elements.get(id);};
  const protocolButtons=['access-openai','access-anthropic','chat-openai','chat-anthropic'].map(id=>{const button=get(id);button.dataset.protocol=id.split('-')[1];return button;});
  const ctx=vm.createContext({document:{getElementById:get,querySelectorAll:selector=>selector==='[data-protocol]'?protocolButtons:[],createElement:element,hidden:false,handlers:{},addEventListener(name,fn){this.handlers[name]=fn;}},
    location:{origin:'http://console.test'},AbortController,TextDecoder,TextEncoder,Option:function(text,value){return {textContent:text,value};},
    setInterval(){},clearTimeout(){},setTimeout(){},fetch,crypto:cryptoImpl,navigator:{clipboard:{writeText:async()=>{}}}});
  vm.runInContext(readFileSync(__dirname+'/web/app.js','utf8'),ctx);
  vm.runInContext("csrf='active-csrf';page='tasks';taskState={items:[{id:'checkin',enabled:true,hours:[9,21],timezone:'Asia/Shanghai',next_at:null}],active_run:null,latest_runs:[]};",ctx);
  return {ctx,get};
}

test('rapid task activation sends once and an unknown response retries the same intent id', async () => {
  let firstReject, postCount=0;
  const bodies=[];
  const response=(status,body)=>({ok:status<400,status,json:async()=>body});
  const {ctx}=taskFixture((url,options={})=>{
    if(url==='/admin/session')return new Promise(()=>{});
    if(url==='/admin/tasks/checkin/runs') {
      postCount++;bodies.push(options.body);
      if(postCount===1)return new Promise((_,reject)=>{firstReject=reject;});
      return response(202,{id:'run-one',task_id:'checkin',status:'running',accounts:[]});
    }
    if(url==='/admin/tasks')return response(200,{items:[{id:'checkin',enabled:true,hours:[9,21],timezone:'Asia/Shanghai',next_at:null}],active_run:null,latest_runs:[]});
    if(url==='/admin/task-runs/run-one')return response(200,{id:'run-one',task_id:'checkin',status:'success',accounts:[],duration_ms:0,log:''});
    if(url==='/admin/task-runs?limit=20')return response(200,{items:[],next_before:null});
    throw new Error('unexpected '+url);
  });
  const first=vm.runInContext("triggerTask('checkin')",ctx);
  await Promise.resolve();
  await vm.runInContext("triggerTask('checkin')",ctx);
  assert.equal(postCount,1,'rapid repeat issued a second POST');
  firstReject(new TypeError('connection lost after request'));
  await first;
  await vm.runInContext("triggerTask('checkin')",ctx);
  assert.equal(postCount,2);
  assert.equal(JSON.parse(bodies[0]).request_id,JSON.parse(bodies[1]).request_id,'retry changed request intent id');
});

test('logout clears task UI state without aborting an accepted-start request', async () => {
  let rejectStart, startSignal;
  const {ctx}=taskFixture((url,options={})=>{
    if(url==='/admin/session')return new Promise(()=>{});
    if(url==='/admin/tasks/checkin/runs'){startSignal=options.signal;return new Promise((_,reject)=>{rejectStart=reject;});}
    throw new Error('unexpected '+url);
  });
  const pending=vm.runInContext("triggerTask('checkin')",ctx);
  await Promise.resolve();
  ctx.document.getElementById('task-log').textContent='private task log';
  vm.runInContext('signedOut()',ctx);
  assert.equal(startSignal,undefined,'page cleanup attached cancellation to the accepted-start request');
  assert.equal(vm.runInContext('taskStarting',ctx),false,'logout left task controls stuck busy');
  assert.equal(ctx.document.getElementById('task-log').textContent,'','logout retained task detail');
  rejectStart(new TypeError('browser logged out'));
  await pending;
});

test('a transient active-task state failure schedules another visible-page poll', async () => {
  let retry;
  const {ctx}=taskFixture(url=>{
    if(url==='/admin/session')return new Promise(()=>{});
    if(url==='/admin/tasks')return Promise.reject(new TypeError('temporary network failure'));
    throw new Error('unexpected '+url);
  });
  ctx.setTimeout=fn=>{retry=fn;return 1;};
  vm.runInContext("taskState.active_run={id:'run-active',task_id:'checkin',status:'running'}",ctx);
  await vm.runInContext('loadTaskState()',ctx);
  assert.equal(typeof retry,'function','transient poll failure stopped automatic updates');
});

test('poll refreshes the former active run after catalog turns inactive', async () => {
  let catalogRead=false;
  const response=body=>({ok:true,status:200,json:async()=>body});
  const {ctx,get}=taskFixture(url=>{
    if(url==='/admin/session')return new Promise(()=>{});
    if(url==='/admin/tasks'){catalogRead=true;return response({items:[{id:'checkin',enabled:true,hours:[9,21],timezone:'Asia/Shanghai',next_at:null}],active_run:null,latest_runs:[]});}
    if(url==='/admin/task-runs/run-active')return response({id:'run-active',task_id:'checkin',status:catalogRead?'success':'running',accounts:[],duration_ms:1,log:''});
    if(url==='/admin/task-runs?limit=20')return response({items:[],next_before:null});
    throw new Error('unexpected '+url);
  });
  vm.runInContext("taskState.active_run={id:'run-active',task_id:'checkin',status:'running'}",ctx);
  await vm.runInContext("loadTaskDetail('run-active')",ctx);
  let scrolls=0;get('task-detail').scrollIntoView=()=>scrolls++;
  await vm.runInContext('pollTaskRun()',ctx);
  assert.match(get('task-detail-title').textContent,/成功/,'final detail was left at running');
  assert.equal(scrolls,0,'background completion scrolled the page');
});

test('visible idle polls detect external history changes without losing loaded pages', async () => {
  let newest=null, historyReads=0, timer;
  const response=body=>({ok:true,status:200,json:async()=>body});
  const old={id:'old',task_id:'checkin',status:'success',accounts:[]};
  const older={id:'older',task_id:'travel',status:'success',accounts:[]};
  const {ctx,get}=taskFixture(url=>{
    if(url==='/admin/session')return new Promise(()=>{});
    if(url==='/admin/tasks')return response({items:[],active_run:newest?.status==='running'?newest:null,latest_runs:newest?[newest]:[]});
    if(url==='/admin/task-runs?limit=20'){historyReads++;return response({items:newest?[newest,old]:[old],next_before:'old'});}
    if(url==='/admin/task-runs?limit=20&before=old')return response({items:[older],next_before:'older'});
    throw new Error('unexpected '+url);
  });
  ctx.setTimeout=(fn,ms)=>{timer={fn,ms};return 1;};
  await vm.runInContext('loadTaskPage();',ctx);
  assert.equal(typeof timer?.fn,'function','idle page stopped polling');
  assert.ok(timer.ms>=2000,'idle poll is unbounded');
  await vm.runInContext('loadTaskHistory()',ctx);
  const row=get('task-history-body').children[0];
  await vm.runInContext('pollTaskRun()',ctx);
  assert.equal(historyReads,1,'unchanged idle tick reloaded history');
  assert.equal(get('task-history-body').children[0],row,'unchanged tick replaced rows');
  newest={id:'external',task_id:'activity',status:'running',accounts:[]};
  await vm.runInContext('pollTaskRun()',ctx);
  assert.equal(vm.runInContext('taskHistory.map(run=>run.id).join(",")',ctx),'external,old,older');
  assert.equal(vm.runInContext('taskBefore',ctx),'older');
  newest={...newest,status:'partial_failure'};
  await vm.runInContext('pollTaskRun()',ctx);
  assert.equal(vm.runInContext('taskHistory[0].status',ctx),'partial_failure');
  assert.equal(vm.runInContext('taskHistory.length',ctx),3);
  assert.equal(historyReads,3);
  ctx.document.hidden=true;ctx.document.handlers.visibilitychange();
  ctx.document.hidden=false;ctx.document.handlers.visibilitychange();
  await new Promise(resolve=>setImmediate(resolve));
  assert.equal(vm.runInContext('taskHistory.length',ctx),3,'returning to visible page discarded loaded history');
  assert.equal(historyReads,3,'visibility resume reloaded unchanged history');
});

test('active polling preserves closed and historical detail ownership', async () => {
  const response=body=>({ok:true,status:200,json:async()=>body});
  const active={id:'active',task_id:'travel',status:'running',accounts:[]};
  const {ctx,get}=taskFixture(url=>{
    if(url==='/admin/session')return new Promise(()=>{});
    if(url==='/admin/tasks')return response({items:[],active_run:active,latest_runs:[active]});
    if(url==='/admin/task-runs?limit=20')return response({items:[active],next_before:null});
    if(url.startsWith('/admin/task-runs/'))return response(url.endsWith('/old')?{id:'old',task_id:'cat',status:'success',accounts:[]}:active);
    throw new Error('unexpected '+url);
  });
  await vm.runInContext("loadTaskState();",ctx);
  await vm.runInContext("loadTaskDetail('active')",ctx);
  get('task-detail-close').handlers.click();
  await vm.runInContext('pollTaskRun()',ctx);
  assert.equal(get('task-detail').hidden,true,'poll reopened closed detail');
  await vm.runInContext("loadTaskDetail('old')",ctx);
  let scrolls=0;get('task-detail').scrollIntoView=()=>scrolls++;
  await vm.runInContext('pollTaskRun()',ctx);
  assert.match(get('task-detail-title').textContent,/夜猫子/,'poll replaced historical selection');
  assert.equal(scrolls,0);
});

test('pagination guards the cursor and a reset invalidates a late earlier page', async () => {
  const pending=[];
  const response=body=>({ok:true,status:200,json:async()=>body});
  const {ctx,get}=taskFixture(url=>{
    if(url==='/admin/session')return new Promise(()=>{});
    return new Promise(resolve=>pending.push({url,resolve}));
  });
  vm.runInContext("taskBefore='cursor';taskHistory=[{id:'cursor',task_id:'cat'}]",ctx);
  const first=vm.runInContext('loadTaskHistory()',ctx);
  const duplicate=vm.runInContext('loadTaskHistory()',ctx);
  assert.equal(pending.length,1,'same cursor requested concurrently');
  await duplicate;
  assert.equal(get('task-more').disabled,true);
  const reset=vm.runInContext('loadTaskHistory(true)',ctx);
  pending[1].resolve(response({items:[{id:'new',task_id:'travel'}],next_before:'new'}));await reset;
  pending[0].resolve(response({items:[{id:'stale',task_id:'cat'}],next_before:null}));await first;
  assert.equal(vm.runInContext('taskHistory.map(run=>run.id).join(",")',ctx),'new');
  assert.equal(vm.runInContext('taskBefore',ctx),'new');
  assert.equal(get('task-more').disabled,false);
});

test('closing detail invalidates a pending response and newest detail wins', async () => {
  const pending=new Map();
  const response=body=>({ok:true,status:200,json:async()=>body});
  const {ctx,get}=taskFixture(url=>{
    if(url==='/admin/session')return new Promise(()=>{});
    if(url.startsWith('/admin/task-runs/'))return new Promise(resolve=>pending.set(url,resolve));
    throw new Error('unexpected '+url);
  });
  const closed=vm.runInContext("loadTaskDetail('run-close')",ctx);
  await Promise.resolve();get('task-detail-close').handlers.click();
  pending.get('/admin/task-runs/run-close')(response({id:'run-close',task_id:'checkin',status:'success',accounts:[],duration_ms:1,log:''}));
  await closed;
  assert.equal(get('task-detail').hidden,true,'closed detail reopened from a late response');

  const older=vm.runInContext("loadTaskDetail('run-a')",ctx);await Promise.resolve();
  const newer=vm.runInContext("loadTaskDetail('run-b')",ctx);await Promise.resolve();
  pending.get('/admin/task-runs/run-b')(response({id:'run-b',task_id:'travel',status:'success',accounts:[],duration_ms:1,log:''}));await newer;
  pending.get('/admin/task-runs/run-a')(response({id:'run-a',task_id:'checkin',status:'failed',accounts:[],duration_ms:1,log:''}));await older;
  assert.match(get('task-detail-title').textContent,/猫猫旅行/,'older detail overwrote the latest selection');

  const navigated=vm.runInContext("loadTaskDetail('run-nav')",ctx);await Promise.resolve();
  vm.runInContext("page='overview';stopTaskReads(true)",ctx);
  pending.get('/admin/task-runs/run-nav')(response({id:'run-nav',task_id:'checkin',status:'success',accounts:[],duration_ms:1,log:''}));await navigated;
  assert.equal(get('task-detail').hidden,true,'navigation allowed a pending detail to reopen');
});

test('equivalent task refresh preserves card nodes for in-flight clicks', () => {
  const {ctx,get}=taskFixture(()=>new Promise(()=>{}));
  vm.runInContext('renderTasks()',ctx);const card=get('task-list').children[0];
  vm.runInContext('taskState=JSON.parse(JSON.stringify(taskState));renderTasks()',ctx);
  assert.equal(get('task-list').children[0],card,'equivalent refresh replaced the clickable card');
});

test('task detail keeps untrusted logs as text and preserves unknown duration', () => {
  const {ctx,get}=taskFixture(()=>new Promise(()=>{}));
  ctx.runFixture={id:'run-x',task_id:'activity',status:'interrupted',started_at:'2026-09-15T10:00:00Z',finished_at:'2026-09-15T10:01:00Z',duration_ms:null,accounts:[{uid:'u1',status:'unknown',detail:'result_unconfirmed',before:null,after:{value:0,observed_at:'2026-09-15T10:00:30Z'},reward:null}],log:'<img src=x onerror=alert(1)>',log_truncated:true};
  vm.runInContext('renderTaskDetail(runFixture)',ctx);
  assert.equal(get('task-log').textContent,'<img src=x onerror=alert(1)>');
  assert.match(get('task-detail-meta').textContent,/耗时：未知/);
  assert.match(get('task-detail-meta').textContent,/观察时间/);
  assert.match(get('task-accounts').textContent,/已确认奖励：未确认/);
  assert.match(get('task-accounts').textContent,/余额 0/);
  assert.equal(get('task-log-truncated').hidden,false);
});

test('logout 503 immediately clears browser secrets and shows cleanup error on login', async () => {
  let finishLogout;
  const {get,ctx,controller} = logoutFixture((url,options) => {
    if(url==='/admin/session')return new Promise(()=>{});
    assert.equal(url,'/admin/logout');
    assert.equal(options.headers['X-CSRF-Token'],'active-csrf');
    return new Promise(resolve=>{finishLogout=resolve;});
  });
  const pending=get('logout').handlers.click();
  assert.equal(get('api-key').value,'','logout left API key visible while cleanup pending');
  assert.equal(get('api-key').type,'password');
  assert.equal(get('admin-key').value,'');
  assert.deepEqual(get('messages').children,[]);
  assert.equal(vm.runInContext('csrf',ctx),'');
  assert.equal(vm.runInContext('history.length',ctx),0);
  assert.equal(vm.runInContext('flowID',ctx),undefined);
  assert.ok(controller.signal.aborted);
  assert.equal(get('console-view').hidden,true);
  assert.equal(get('login-view').hidden,false);
  finishLogout({ok:false,status:503,json:async()=>({error:'核心授权流程取消失败，管理会话已退出'})});
  await pending;
  assert.equal(get('login-error').textContent,'核心授权流程取消失败，管理会话已退出');
  assert.equal(get('api-key').value,'');
});

test('an access response started before logout cannot restore the revealed key', async () => {
  let finishAccess;
  const {get} = logoutFixture(url => {
    if(url==='/admin/session')return new Promise(()=>{});
    if(url==='/admin/access')return new Promise(resolve=>{finishAccess=resolve;});
    assert.equal(url,'/admin/logout');
    return Promise.resolve({ok:true,status:200,json:async()=>({ok:true})});
  });
  const reveal=get('reveal-key').handlers.click();
  await get('logout').handlers.click();
  finishAccess({ok:true,status:200,json:async()=>({api_key:'late-private-key'})});
  await reveal;
  assert.equal(get('api-key').value,'','late access response repopulated logged-out browser');
  assert.equal(get('login-view').hidden,false);
});

test('stale 401 responses cannot revoke a newly logged-in session', async () => {
  for (const path of ['status','logout']) {
    let finishOld;
    const {get,ctx} = logoutFixture(url => {
      if(url==='/admin/session')return new Promise(()=>{});
      if(url==='/admin/'+path)return new Promise(resolve=>{finishOld=resolve;});
      assert.equal(url,'/admin/logout');
      return Promise.resolve({ok:true,status:200,json:async()=>({ok:true})});
    });
    const old=path==='logout'?get('logout').handlers.click():vm.runInContext("jsonAPI('status')",ctx).catch(error=>error);
    if(path!=='logout')await get('logout').handlers.click();
    vm.runInContext("csrf='new-session-csrf';",ctx);
    get('login-view').hidden=true;get('console-view').hidden=false;get('api-key').value='new-session-key';
    finishOld({ok:false,status:401,json:async()=>({error:'old session expired'})});
    await old;
    assert.equal(vm.runInContext('csrf',ctx),'new-session-csrf',path+' 401 revoked new session');
    assert.equal(get('console-view').hidden,false);
    assert.equal(get('login-view').hidden,true);
    assert.equal(get('api-key').value,'new-session-key');
  }
});

test('stale copy responses never unmask a future key or trigger clipboard fallback', async () => {
  for (const failureStage of ['access','clipboard','clipboard-success']) {
    let finishOld;
    const {get,ctx} = logoutFixture(url => {
      if(url==='/admin/session')return new Promise(()=>{});
      if(url==='/admin/access')return new Promise(resolve=>{finishOld=resolve;});
      assert.equal(url,'/admin/logout');
      return Promise.resolve({ok:true,status:200,json:async()=>({ok:true})});
    });
    ctx.navigator={clipboard:{writeText:()=>new Promise((resolve,reject)=>{finishOld=failureStage==='clipboard-success'?resolve:reject;})}};
    let selected=false;get('api-key').select=()=>{selected=true;};
    if(failureStage==='access')get('api-key').value='';
    const copy=get('copy-key').handlers.click();
    await get('logout').handlers.click();
    vm.runInContext("csrf='new-session-csrf';",ctx);
    get('login-view').hidden=true;get('console-view').hidden=false;
    get('api-key').value='future-private-key';get('api-key').type='password';
    if(failureStage==='access')finishOld({ok:true,status:200,json:async()=>({api_key:'old-private-key'})});
    else finishOld(new Error('old clipboard permission denied'));
    await copy;
    assert.equal(get('api-key').type,'password',failureStage+' fallback unmasked future key');
    assert.equal(get('api-key').value,'future-private-key');
    assert.equal(selected,false);
    assert.equal(get('notice').textContent,'');
  }
});

test('current-session clipboard denial still offers manual copy', async () => {
  const {get,ctx}=logoutFixture(()=>new Promise(()=>{}));
  ctx.navigator={clipboard:{writeText:async()=>{throw new Error('permission denied');}}};
  let selected=false;get('api-key').select=()=>{selected=true;};get('api-key').type='password';
  await get('copy-key').handlers.click();
  assert.equal(get('api-key').type,'text');
  assert.equal(selected,true);
  assert.match(get('notice').textContent,/手动复制/);
});

test('a current-session 401 still clears local authentication', async () => {
  const {get,ctx}=logoutFixture(url=>url==='/admin/session'?new Promise(()=>{}):Promise.resolve({ok:false,status:401}));
  await assert.rejects(vm.runInContext("jsonAPI('status')",ctx),/管理会话已过期/);
  assert.equal(vm.runInContext('csrf',ctx),'');
  assert.equal(get('api-key').value,'');
  assert.equal(get('console-view').hidden,true);
  assert.equal(get('login-view').hidden,false);
});

function chatFixture(chunks, status=200) {
  const requests=[];
  const fixture=taskFixture(async (url,options)=>{
    if(url==='/admin/session'||url==='/admin/status')return new Promise(()=>{});
    requests.push({url,...options});
    let index=0;
    return {ok:status===200,status,json:async()=>({error:{message:'安全错误'}}),body:{getReader:()=>({read:async()=>index<chunks.length?{value:chunks[index++],done:false}:{done:true}})}};
  });
  fixture.get('model').value='global:claude-sonnet-4.6';
  fixture.get('access-model').value='global:claude-sonnet-4.6';
  fixture.get('max-tokens').value='1024';
  fixture.submit=()=>{fixture.get('prompt').value='你好';return fixture.get('chat-form').handlers.submit({preventDefault(){}});};
  return {...fixture,requests};
}
const sse=(...parts)=>new TextEncoder().encode(parts.map(part=>'data: '+(typeof part==='string'?part:JSON.stringify(part))+'\r\n\r\n').join(''));
const textDelta={type:'content_block_delta',delta:{type:'text_delta',text:'你好世界'}};
const stopMessage={type:'message_stop'};

test('protocol buttons synchronize, preserve same-protocol history and generate safe real-model access examples', async()=>{
  const {ctx,get}=taskFixture(()=>new Promise(()=>{}));
  assert.equal(get('base-url').value,'http://console.test/v1');
  assert.equal(get('api-endpoint').value,'http://console.test/v1/chat/completions');
  assert.doesNotMatch(get('protocol-support').textContent,/Beta 测试/);
  assert.equal(get('copy-example').disabled,true);
  assert.equal(get('go-chat').disabled,true);
  vm.runInContext("modelList=[{id:'global:real-model'}];history=[{role:'user',content:'old'}]",ctx);
  get('access-model').value='global:real-model';get('api-key').value='never-copy-this-secret';
  get('access-anthropic').handlers.click();
  assert.equal(vm.runInContext('history.length',ctx),0);
  assert.equal(get('base-url').value,'http://console.test');
  assert.equal(get('api-endpoint').value,'http://console.test/v1/messages');
  assert.match(get('protocol-support').textContent,/Beta 测试/);
  assert.match(get('api-example').textContent,/anthropic-version: 2023-06-01/);
  assert.match(get('api-example').textContent,/global:real-model/);
  assert.doesNotMatch(get('api-example').textContent,/never-copy-this-secret/);
  assert.equal(get('effort').hidden,true);assert.equal(get('max-tokens').hidden,false);
  assert.equal(get('chat-anthropic')['aria-pressed'],'true');
  const conversation=vm.runInContext('conversation',ctx);
  vm.runInContext("history=[{role:'user',content:'keep'}]",ctx);
  get('chat-anthropic').handlers.click();assert.equal(vm.runInContext('history.length',ctx),1);
  assert.equal(vm.runInContext('conversation',ctx),conversation);
  let copied;ctx.navigator.clipboard.writeText=async value=>{copied=value;};
  await get('copy-example').handlers.click();assert.equal(copied,get('api-example').textContent);
  get('go-chat').handlers.click();assert.equal(vm.runInContext('page',ctx),'chat');assert.equal(get('model').value,'global:real-model');
  get('prompt').value='private draft';get('usage').textContent='private stream error';
  vm.runInContext('signedOut()',ctx);assert.equal(vm.runInContext('protocol',ctx),'openai');assert.equal(get('api-key').value,'');assert.equal(get('prompt').value,'');assert.equal(get('usage').textContent,'用量将在上游返回后显示');
  assert.doesNotMatch(get('protocol-support').textContent,/Beta 测试/);
});

test('Anthropic fragmented UTF8 stream uses management envelope and preserves real usage and multi-turn text', async()=>{
  const bytes=sse(textDelta,{type:'message_delta',usage:{input_tokens:0,output_tokens:2}},stopMessage);
  const {ctx,get,requests,submit}=chatFixture(Array.from(bytes,byte=>new Uint8Array([byte])));
  vm.runInContext("setProtocol('anthropic')",ctx);get('effort').value='high';get('max-tokens').value='9000';
  await submit();
  assert.equal(requests[0].url,'/admin/messages');
  const body=JSON.parse(requests[0].body);
  assert.ok(body.conversation_id.startsWith('web-'));
  assert.deepEqual(body.request,{model:'global:claude-sonnet-4.6',max_tokens:9000,messages:[{role:'user',content:'你好'}],stream:true});
  assert.equal(vm.runInContext('history[1].content',ctx),'你好世界');assert.match(get('usage').textContent,/输入 0 · 输出 2/);
  await submit();assert.equal(JSON.parse(requests[1].body).request.messages.length,3);
});

test('Anthropic unknown usage, stream error and premature EOF never invent completion or usage', async()=>{
  for(const [parts,success,usageText] of [
    [[textDelta,{type:'message_delta',usage:{input_tokens:null,output_tokens:null}},stopMessage],true,'上游未返回用量'],
    [[textDelta,{type:'message_delta',usage:{input_tokens:null,output_tokens:0}},stopMessage],true,'输入 — · 输出 0'],
    [[textDelta,{type:'error',error:{message:'安全的上游错误'}}],false,'安全的上游错误'],
    [[textDelta],false,'响应提前中断'],
    [[textDelta,'[DONE]'],false,'响应提前中断']]) {
    const {ctx,get,requests,submit}=chatFixture([sse(...parts)]);vm.runInContext("setProtocol('anthropic')",ctx);await submit();
    assert.equal(vm.runInContext('history.length',ctx),success?2:0);assert.ok(get('usage').textContent.includes(usageText));assert.ok(requests[0].signal.aborted);
    assert.equal(get('effort').disabled,true);assert.equal(get('model').disabled,false);
  }
});

test('Anthropic validates only safe positive integers; OpenAI retains reasoning, tools and DONE behavior', async()=>{
  for(const value of ['','0','-1','1.5','9007199254740992']) {
    const {ctx,get,requests,submit}=chatFixture([]);vm.runInContext("setProtocol('anthropic')",ctx);get('max-tokens').value=value;await submit();assert.equal(requests.length,0,value);assert.match(get('notice').textContent,/正整数/);
  }
  const {ctx,get,requests,submit}=chatFixture([sse({choices:[{delta:{content:'answer',reasoning_content:'reason',tool_calls:[{index:0,function:{name:'tool',arguments:'{}'}}]}}]},'[DONE]')]);
  get('max-tokens').value='invalid';get('effort').value='high';await submit();
  assert.equal(requests[0].url,'/admin/chat');assert.equal(JSON.parse(requests[0].body).reasoning_effort,'high');assert.equal(vm.runInContext('history[1].content',ctx),'answer');assert.match(get('messages').children[1].children.at(-1).textContent,/仅展示，不执行/);
});

test('active requests lock both protocols and models, ignore refresh, and stop without success history', async()=>{
  let finishRead,signal;
  const {ctx,get}=taskFixture(async(url,options)=>{
    if(url==='/admin/session'||url==='/admin/status')return new Promise(()=>{});
    if(url==='/admin/models')return {ok:true,status:200,json:async()=>({data:[{id:'replacement'}]})};
    signal=options.signal;return {ok:true,status:200,body:{getReader:()=>({read:()=>new Promise(resolve=>{finishRead=resolve;})})}};
  });
  vm.runInContext("modelList=[{id:'old',reasoning_supported_efforts:['high']}];setProtocol('anthropic')",ctx);
  get('model').value='old';get('access-model').value='old';get('max-tokens').value='1024';get('prompt').value='hello';
  const pending=get('chat-form').handlers.submit({preventDefault(){}});await new Promise(resolve=>setImmediate(resolve));
  for(const id of ['model','access-model','clear-chat','chat-openai','access-anthropic'])assert.equal(get(id).disabled,true,id);
  get('chat-openai').handlers.click();assert.equal(vm.runInContext('protocol',ctx),'anthropic');
  await vm.runInContext('refreshModels()',ctx);assert.equal(get('model').value,'old');assert.equal(get('access-model').value,'old');
  get('stop-chat').handlers.click();assert.ok(signal.aborted);finishRead({done:true});await pending;
  assert.equal(vm.runInContext('history.length',ctx),0);assert.equal(get('usage').textContent,'已停止生成');
  get('chat-openai').handlers.click();assert.equal(get('effort').disabled,false);
});

test('Anthropic authentication expiry resets protocol and browser secrets', async()=>{
  const {ctx,get,submit}=chatFixture([],401);vm.runInContext("setProtocol('anthropic')",ctx);get('api-key').value='private';await submit();
  assert.equal(vm.runInContext('protocol',ctx),'openai');assert.equal(vm.runInContext('history.length',ctx),0);assert.equal(get('api-key').value,'');assert.equal(get('console-view').hidden,true);
});

test('an empty Anthropic text response does not fabricate a tool call in history', async()=>{
  const {ctx,submit}=chatFixture([sse(stopMessage)]);vm.runInContext("setProtocol('anthropic')",ctx);await submit();
  assert.equal(vm.runInContext('history[1].content',ctx),'');
});

test('refresh populates both model selects from server data, preserves choices and disables empty access', async()=>{
  let models=[{id:'global:first'},{id:'global:second'}];
  const {ctx,get}=taskFixture(url=>url==='/admin/models'?Promise.resolve({ok:true,status:200,json:async()=>({data:models})}):new Promise(()=>{}));
  vm.runInContext("accounts=[{realm:'global'}]",ctx);
  await vm.runInContext('refreshModels()',ctx);
  assert.deepEqual(get('access-model').children.map(option=>option.value),models.map(model=>model.id));
  get('access-model').value='global:second';get('access-model').handlers.change();
  assert.match(get('api-example').textContent,/global:second/);
  await vm.runInContext('refreshModels()',ctx);assert.equal(get('access-model').value,'global:second');assert.equal(get('model').value,'global:first');
  models=[];await vm.runInContext('refreshModels()',ctx);assert.equal(get('copy-example').disabled,true);assert.equal(get('go-chat').disabled,true);
});

test('access curl quotes JSON model strings without executing shell metacharacters',()=>{
  const {ctx,get}=taskFixture(()=>new Promise(()=>{}));
  const model='global:quote\'$(not-executed)"';ctx.actualModel=model;
  vm.runInContext('modelList=[{id:actualModel}]',ctx);get('access-model').value=model;vm.runInContext('renderAccess()',ctx);
  const quoted=get('api-example').textContent.split("  -d '")[1].slice(0,-1);
  assert.equal(JSON.parse(quoted.replace(/'\\''/g,"'")).model,model);
  assert.ok(quoted.includes("'\\''"));
});


function desktopFixture(url, sessionStatus=401) {
  const elements=new Map(), requests=[];
  const element=()=>({value:'',type:'password',hidden:false,disabled:false,textContent:'',handlers:{},
    addEventListener(name,fn){this.handlers[name]=fn;},replaceChildren(){},append(){},querySelector(){return {disabled:false};}});
  const get=id=>{if(!elements.has(id))elements.set(id,element());return elements.get(id);};
  const location=new URL(url), replacements=[];
  const ctx=vm.createContext({URL,location,document:{getElementById:get,querySelectorAll:()=>[]},
    history:{state:{keep:true},replaceState(state,title,path){replacements.push(path);location.href=new URL(path,location).href;}},
    localStorage:{setItem(){throw new Error('must not persist credentials');}},
    setInterval(){},clearTimeout(){},fetch:async(path,options)=>{
      requests.push({path,...options});
      if(path==='/admin/session')return {status:sessionStatus,ok:sessionStatus===200,json:async()=>({csrf:'existing-csrf',global_enabled:false})};
      if(path==='/admin/login')return {status:401,ok:false,json:async()=>({error:'invalid key'})};
      return {status:200,ok:true,json:async()=>({accounts:[],data:[]})};
    }});
  const html=readFileSync(__dirname+'/web/index.html','utf8');
  for(const match of html.matchAll(/<script[^>]*src="([^"]+)"[^>]*><\/script>/g)) {
    vm.runInContext(readFileSync(__dirname+'/web'+match[1],'utf8'),ctx);
  }
  return {get,ctx,requests,location,replacements};
}

test('desktop URL key is decoded and scrubbed before waiting for session without automatic login',async()=>{
  const fixture=desktopFixture('http://127.0.0.1:7863/?keep=one&admin_key=a%2Bb%2Fc%3D%E9%92%A5%E5%8C%99&keep=two#accounts');
  assert.equal(fixture.location.href,'http://127.0.0.1:7863/?keep=one&keep=two#accounts');
  await new Promise(setImmediate);
  assert.equal(fixture.get('admin-key').value,'a+b/c=钥匙');
  assert.equal(fixture.get('admin-key').type,'password');
  assert.equal(fixture.requests.some(request=>request.method==='POST'),false);
  assert.equal(fixture.ctx.takeDesktopAdminKey?.()||'','');
});

test('non-loopback pages discard URL credentials without prefill',async()=>{
  for(const host of ['console.example','127.0.0.1.evil.test','192.168.1.2']) {
    const fixture=desktopFixture('http://'+host+'/?admin_key=secret&admin_key=another#keep');
    assert.equal(fixture.location.search,'');
    await new Promise(setImmediate);
    assert.equal(fixture.get('admin-key').value,'');
    assert.equal(fixture.location.hash,'#keep');
  }
});

test('desktop key is one-shot across login failure, logout and existing sessions',async()=>{
  const fixture=desktopFixture('http://localhost:7863/?admin_key=one-shot');
  await new Promise(setImmediate);
  assert.equal(fixture.get('admin-key').value,'one-shot');
  await fixture.get('login-form').handlers.submit({preventDefault(){},submitter:{}});
  const request=fixture.requests.find(request=>request.path==='/admin/login');
  assert.deepEqual(JSON.parse(request.body),{key:'one-shot'});
  assert.equal(fixture.get('admin-key').value,'');
  vm.runInContext('signedOut()',fixture.ctx);
  assert.equal(fixture.get('admin-key').value,'');
  assert.equal(fixture.ctx.takeDesktopAdminKey?.()||'','');
  const existing=desktopFixture('http://[::1]:7863/?admin_key=discard',200);
  await new Promise(setImmediate);
  assert.equal(existing.get('admin-key').value,'');
  assert.equal(existing.get('login-view').hidden,true);
  assert.equal(existing.ctx.takeDesktopAdminKey?.()||'','');
});

test('normal manual login keeps its existing retry behavior without a desktop key',async()=>{
  const fixture=desktopFixture('http://console.example/');
  await new Promise(setImmediate);
  fixture.get('admin-key').value='manual-key';
  await fixture.get('login-form').handlers.submit({preventDefault(){},submitter:{}});
  assert.equal(fixture.get('admin-key').value,'manual-key');
});
