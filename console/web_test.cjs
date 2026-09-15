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
