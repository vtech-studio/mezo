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
})();
