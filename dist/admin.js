'use strict';
(() => {
  const brl = new Intl.NumberFormat('pt-BR', {style:'currency', currency:'BRL'});
  function format(input) {
    const digits = input.value.replace(/\D/g, '').replace(/^0+(?=\d)/, '');
    const cents = digits ? Number(digits) : 0;
    const valid = Number.isSafeInteger(cents) && cents <= 10000000;
    input.setCustomValidity(valid ? '' : 'Use um preço de até R$ 100.000,00.');
    if (digits && valid) input.value = brl.format(cents / 100);
    if (!digits) input.value = '';
  }
  document.querySelectorAll('[data-money]').forEach(format);
  document.addEventListener('input', event => {
    if (!event.target.matches('[data-money]')) return;
    format(event.target);
    event.target.setSelectionRange(event.target.value.length, event.target.value.length);
  });
  document.addEventListener('focusin', event => {
    if (event.target.matches('[data-money]')) event.target.select();
  });

  document.addEventListener('change', event => {
    const input = event.target;
    if (!input.matches('[data-image-form] input[type="file"]')) return;
    input.setCustomValidity(input.files[0]?.size > 3 * 1024 * 1024 ? 'Escolha uma imagem de até 3 MB.' : '');
    input.reportValidity();
  });
  document.addEventListener('htmx:beforeSwap', event => {
    const {xhr, requestConfig} = event.detail;
    if (requestConfig?.elt?.matches('[data-image-form]') && xhr.status >= 400 && xhr.getResponseHeader('Content-Type')?.includes('text/html')) {
      event.detail.shouldSwap = true; event.detail.isError = false;
    }
  });
  document.addEventListener('htmx:beforeRequest', event => {
    if (event.detail.elt?.matches('[data-image-form]')) {
      const target = document.querySelector(event.detail.elt.getAttribute('hx-target'));
      if (target) target.textContent = 'Enviando imagem…';
    }
  });
  document.addEventListener('htmx:afterRequest', event => {
    const form = event.detail.elt;
    if (form?.matches('[data-image-form]') && event.detail.xhr.status === 0) {
      const target = document.querySelector(form.getAttribute('hx-target'));
      if (target) target.textContent = 'Não foi possível confirmar o envio. Confira a conexão e tente novamente.';
    }
  });
  const login = document.getElementById('admin-login');
  if (!login) return;
  const result = document.getElementById('login-result'), button = login.querySelector('button');
  const cooldowns = new Map();
  let submittedEmail = '';
  const email = () => login.elements.email.value.trim().toLowerCase();
  function tick() {
    const seconds = Math.max(0, Math.ceil(((cooldowns.get(email()) || 0) - Date.now()) / 1000));
    let message = result.querySelector('[data-login-message]');
    if (seconds > 0 && !message) {
      const notice = document.createElement('p'); notice.className = 'notice error'; notice.setAttribute('role', 'alert');
      message = document.createElement('span'); message.setAttribute('data-login-message', ''); notice.append(message); result.replaceChildren(notice);
    }
    button.disabled = seconds > 0 || login.classList.contains('htmx-request');
    button.textContent = seconds > 0 ? `Tentar novamente em ${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}` : 'Entrar →';
    if (seconds > 0 && message) message.textContent = `Muitas tentativas para este acesso. Aguarde ${Math.floor(seconds / 60)} min e ${seconds % 60} s antes de tentar novamente.`;
    if (!seconds && cooldowns.has(email())) { cooldowns.delete(email()); if (message) message.textContent = 'Você já pode tentar novamente.'; }
  }
  function readCooldown(account = email()) {
    const seconds = Number(result.querySelector('[data-retry-seconds]')?.dataset.retrySeconds || 0);
    if (seconds > 0) cooldowns.set(account, Date.now() + seconds * 1000);
    if (account !== email()) result.replaceChildren();
    tick();
  }
  document.addEventListener('htmx:beforeSwap', event => {
    // HTMX normally skips 4xx responses. Login errors are escaped HTML fragments.
    if (event.detail.target?.id === 'login-result' && event.detail.xhr.status >= 400 && event.detail.xhr.status < 600 && event.detail.xhr.getResponseHeader('Content-Type')?.startsWith('text/html')) {
      event.detail.shouldSwap = true; event.detail.isError = false;
    }
  });
  document.addEventListener('htmx:beforeRequest', event => {
    if (event.detail.elt !== login) return;
    if ((cooldowns.get(email()) || 0) > Date.now()) event.preventDefault();
    else submittedEmail = email();
  });
  document.addEventListener('htmx:afterSwap', event => { if (event.detail.target?.id === 'login-result') readCooldown(submittedEmail || email()); });
  document.addEventListener('htmx:afterRequest', event => {
    if (event.detail.elt !== login) return;
    if (!event.detail.xhr.status) {
      const notice = document.createElement('p'); notice.className = 'notice error'; notice.setAttribute('role', 'alert'); notice.textContent = 'Sem resposta do servidor. Confira a conexão e tente novamente.'; result.replaceChildren(notice);
    }
    setTimeout(tick, 0);
  });
  login.elements.email.addEventListener('input', () => { result.replaceChildren(); tick(); });
  readCooldown(); setInterval(tick, 1000);
})();
