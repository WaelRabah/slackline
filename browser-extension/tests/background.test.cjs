const {test}=require('node:test');
const assert=require('node:assert/strict');
const vm=require('node:vm');
const fs=require('node:fs');
const source=fs.readFileSync(require('node:path').join(__dirname,'../background.js'),'utf8');
function fixture(){
 const state={};const sent=[];
 const chrome={runtime:{onMessage:{addListener(){}}},storage:{session:{async get(){return state},async set(value){Object.assign(state,value)},async remove(key){delete state[key]}}},tabs:{async update(){},async get(id){return {id}}},cookies:{async get(){return {domain:'.slack.com',secure:true,value:'xoxd-fixture%2F'}}}};
 const context=vm.createContext({chrome,URL,Date,AbortSignal,fetch:async(url,options)=>{sent.push({url,body:JSON.parse(options.body)});return {ok:true}}});
 vm.runInContext(source,context);
 return {context,state,sent,chrome,handle:context.handle};
}
test('only consented login tab can send to the one-time local callback',async()=>{
 const f=fixture();const state='a'.repeat(64);
 const local={url:'http://127.0.0.1:12345/#'+state,tab:{id:7}};
 assert.equal((await f.handle({type:'begin',state,startURL:'https://slack.com/signin'},local)).ok,true);
 const msg={type:'capture',team:'TTEST',token:'xoxc-fixture'};
 assert.equal((await f.handle(msg,{url:'https://app.slack.com/client/TTEST',tab:{id:8}})).ok,undefined);
 assert.equal(f.sent.length,0);
 assert.equal((await f.handle(msg,{url:'https://app.slack.com/client/TTEST',tab:{id:7}})).ok,true);
 assert.equal(f.sent[0].url,'http://127.0.0.1:12345/callback');
 assert.equal(f.sent[0].body.cookie,'xoxd-fixture%2F');
 assert.equal(f.state.pending,undefined);
});
test('rejects lookalike Slack origins and mismatched nonce',async()=>{
 const f=fixture();
 assert.equal(f.context.slackClient('https://slack.com.attacker.test/client/TTEST'),null);
 assert.equal(f.context.slackClient('http://app.slack.com/client/TTEST'),null);
 const result=await f.handle({type:'begin',state:'b'.repeat(64),startURL:'https://slack.com/signin'},{url:'http://127.0.0.1:12345/#'+'a'.repeat(64),tab:{id:7}});
 assert.ok(result.error);assert.equal(f.state.pending,undefined);
});
