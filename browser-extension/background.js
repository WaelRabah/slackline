let sending = false;
function slackClient(raw) {
  try {
    const url = new URL(raw);
    if (url.protocol !== 'https:' || !(url.hostname === 'slack.com' || url.hostname.endsWith('.slack.com')) || (url.port && url.port !== '443') || url.username || url.password) return null;
    return url.pathname.match(/^\/client\/(T[A-Z0-9]+)(?:\/|$)/)?.[1] || null;
  } catch (_) { return null; }
}
async function pendingFor(sender) {
  const {pending} = await chrome.storage.session.get('pending');
  if (!pending || pending.expires < Date.now()) return null;
  let tab = sender.tab;
  for (let depth = 0; tab && depth < 6; depth++) {
    if (tab.id === pending.tabId) return pending;
    if (typeof tab.openerTabId !== 'number') break;
    try { tab = await chrome.tabs.get(tab.openerTabId); } catch (_) { break; }
  }
  return null;
}
async function handle(message,sender) {
  if (message.type === 'begin') {
    const page = new URL(sender.url);
    if (page.protocol !== 'http:' || page.hostname !== '127.0.0.1' || !page.port || page.pathname !== '/' || !/^[a-f0-9]{64}$/.test(message.state) || page.hash.slice(1) !== message.state || !Number.isInteger(sender.tab?.id)) return {error:'Invalid local login page'};
    const start = new URL(message.startURL);
    if (start.protocol !== 'https:' || !(start.hostname === 'slack.com' || start.hostname.endsWith('.slack.com')) || start.port || start.username || start.password) return {error:'Invalid Slack URL'};
    await chrome.storage.session.set({pending:{callback:page.origin+'/callback',state:message.state,tabId:sender.tab.id,expires:Date.now()+15*60*1000}});
    await chrome.tabs.update(sender.tab.id,{url:start.href});
    return {ok:true};
  }
  const team = slackClient(sender.url);
  if (!team) return {pending:false};
  const pending = await pendingFor(sender);
  if (message.type === 'status') return {pending:!!pending};
  if (message.type !== 'capture' || !pending || sending || team !== message.team || typeof message.token !== 'string' || !message.token.startsWith('xoxc-')) return {error:'No matching login'};
  const cookie = await chrome.cookies.get({url:'https://slack.com/api/',name:'d'});
  if (!cookie?.value || !cookie.secure || !['slack.com','.slack.com'].includes(cookie.domain)) return {error:'Slack cookie not ready'};
  sending = true;
  try {
    const response = await fetch(pending.callback,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({state:pending.state,token:message.token,cookie:cookie.value}),signal:AbortSignal.timeout(10000)});
    if (!response.ok) throw new Error();
    await chrome.storage.session.remove('pending');
    return {ok:true};
  } catch (_) {
    await chrome.storage.session.remove('pending');
    return {error:'Local login stopped; restart auth login'};
  } finally { sending = false; }
}
chrome.runtime.onMessage.addListener((message,sender,reply) => {
  handle(message,sender).then(reply,() => reply({error:'Login helper failed'}));
  return true;
});
