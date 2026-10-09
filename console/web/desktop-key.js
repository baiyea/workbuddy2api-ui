// Run before page resources; never leave a credential in the address bar or storage.
(() => {
 const url = new URL(location.href);
 let key = '';
 if (url.searchParams.has('admin_key')) {
  const supplied = url.searchParams.get('admin_key');
  url.searchParams.delete('admin_key');
  history.replaceState(history.state, '', url.pathname + url.search + url.hash);
  if (['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)) key = supplied;
 }
 globalThis.takeDesktopAdminKey = () => {
  const value = key;
  key = '';
  return value;
 };
})();
