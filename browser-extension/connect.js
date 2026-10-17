(() => {
  if (!document.querySelector('meta[name="slackline-login"][content="1"]')) return;
  const button = document.getElementById('connect');
  if (!button || !/^[a-f0-9]{64}$/.test(location.hash.slice(1))) return;
  document.getElementById('status').textContent = 'Helper connected. Continue to Slack to choose your session.';
  button.disabled = false;
  button.addEventListener('click', async () => {
    button.disabled = true;
    try {
      const result = await chrome.runtime.sendMessage({type:'begin', state:location.hash.slice(1), startURL:button.dataset.startUrl});
      if (result?.error) throw new Error();
    } catch (_) {
      document.getElementById('status').textContent = 'Could not connect. Restart auth login and reload the helper extension.';
      button.disabled = false;
    }
  });
})();
