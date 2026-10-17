(() => {
  let busy = false;
  const timer = setInterval(async () => {
    if (busy) return;
    busy = true;
    try {
      const status = await chrome.runtime.sendMessage({type:'status'});
      if (!status?.pending) return;
      const team = location.pathname.match(/^\/client\/(T[A-Z0-9]+)(?:\/|$)/)?.[1];
      if (!team) return;
      const token = JSON.parse(localStorage.getItem('localConfig_v2') || '{}').teams?.[team]?.token;
      if (typeof token !== 'string' || !token.startsWith('xoxc-')) return;
      const result = await chrome.runtime.sendMessage({type:'capture', token, team});
      if (result?.ok) clearInterval(timer);
    } catch (_) { /* Slack can still be loading or navigating. Never log session data. */ }
    finally { busy = false; }
  }, 750);
  setTimeout(() => clearInterval(timer), 15 * 60 * 1000);
})();
