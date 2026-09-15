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
    location:{origin:'http://console.test'}, AbortController, TextDecoder,
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
      replaceChildren(...children){this.children=children;},querySelector(){return null;},scrollIntoView(){},select(){}};
  }
  const get=id=>{if(!elements.has(id))elements.set(id,element());return elements.get(id);};
  const ctx=vm.createContext({document:{getElementById:get,querySelectorAll:()=>[],createElement:element,hidden:false,handlers:{},addEventListener(name,fn){this.handlers[name]=fn;}},
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
