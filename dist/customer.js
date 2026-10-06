'use strict';
let customerBasket=[], customerPending=null, customerCartScope='', customerSending=false, customerOrderNote='';
const customerCartKey=()=>`mezo-cart:${tenantSlug||'preview'}:${liveSessionId||'demo'}`;
function saveCustomerCart(){
  try{sessionStorage.setItem(customerCartKey(),JSON.stringify({items:customerBasket,pending:customerPending,note:customerOrderNote}));}catch{}
}
function restoreCustomerCart(){
  const key=customerCartKey();if(customerCartScope===key)return;customerCartScope=key;
  customerBasket=[];customerPending=null;customerOrderNote='';
  try{
    const saved=JSON.parse(sessionStorage.getItem(key));
    if(saved&&Array.isArray(saved.items)&&saved.items.length<=40&&saved.items.every(i=>typeof i.menuId==='string'&&Number.isInteger(i.qty)&&i.qty>0&&i.qty<=100&&typeof i.note==='string'&&i.note.length<=200)){
      customerBasket=saved.items;customerOrderNote=String(saved.note||'').slice(0,300);
      const p=saved.pending;
      if(p&&typeof p.key==='string'&&p.key.length>=16&&p.key.length<=80&&typeof p.note==='string'&&p.note.length<=300&&Number.isSafeInteger(p.expectedSubtotal)&&p.expectedSubtotal>=0&&JSON.stringify(p.items)===JSON.stringify(customerBasket.map(i=>({menuId:i.menuId,qty:i.qty,note:i.note}))))customerPending=p;
    }
  }catch{}
}
function customerSubtotal(){return customerPending?.expectedSubtotal??customerBasket.reduce((sum,i)=>sum+(menu.find(m=>m.id===i.menuId)?.price??0)*i.qty,0);}
function renderCartBar(){
  document.querySelector('.customer-cart-bar')?.remove();main.classList.toggle('has-cart',route==='cardapio'&&customerBasket.length>0);
  if(route!=='cardapio'||!customerBasket.length)return;
  const bar=document.createElement('div');bar.className='customer-cart-bar';
  bar.innerHTML=`<button class="primary-button" data-action="review-order"><span>${customerPending?'Confirmar envio':'Ver meu pedido'} · ${customerBasket.reduce((n,i)=>n+i.qty,0)} ${customerBasket.reduce((n,i)=>n+i.qty,0)===1?'item':'itens'}</span><strong>${money(customerSubtotal())}</strong></button>`;
  document.body.append(bar);
}
function openCustomerDish(dish){
  if(customerPending){openCustomerCart();return;}
  openSheet(dish.name,`<div class="dish-detail-heading"><span>${escapeHTML(dish.category)}</span><strong>${money(dish.price)}</strong></div><p class="detail-description">${escapeHTML(dish.description)}</p>${(dish.tags||[]).map(t=>`<span class="ingredient-tag">${escapeHTML(t)}</span>`).join('')}<label class="field-label" for="dish-qty">Quantidade</label><div class="cart-counter"><button data-action="dish-quantity" data-delta="-1" aria-label="Diminuir quantidade">−</button><input id="dish-qty" type="number" value="1" min="1" max="100" inputmode="numeric" aria-label="Quantidade de ${escapeHTML(dish.name)}"><button data-action="dish-quantity" data-delta="1" aria-label="Aumentar quantidade">+</button></div><label class="field-label" for="dish-note">Observação para a cozinha (opcional)</label><textarea id="dish-note" maxlength="200" placeholder="Ex.: sem cebola."></textarea><button class="primary-button" data-action="add-customer-item" data-id="${escapeHTML(dish.id)}">Adicionar ao pedido ${icon('arrow')}</button>`);
}
function openCustomerCart(message=''){
  if(!customerBasket.length){closeSheet();toast('Escolha os itens no cardápio.');return;}
  const unavailable=customerBasket.some(i=>!menu.some(m=>m.id===i.menuId));
  openSheet(`Pedido · mesa ${tableName(tableNumber)}`,`<p class="sheet-description">Ao confirmar, os itens entram na conta da mesa e seguem para a cozinha.</p>${customerBasket.map((i,index)=>{const item=menu.find(m=>m.id===i.menuId);return `<div class="customer-cart-item"><div><strong>${escapeHTML(customerPending?i.reviewName||i.name:item?.name||i.name||'Item indisponível')}</strong><small>${customerPending&&Number.isSafeInteger(i.reviewPrice)?money(i.reviewPrice):item?money(item.price):'Indisponível'}${i.note?' · '+escapeHTML(i.note):''}</small></div><div class="cart-counter"><button data-action="cart-quantity" data-index="${index}" data-delta="-1" aria-label="Diminuir ${escapeHTML(item?.name||i.name)}" ${customerPending?'disabled':''}>−</button><span>${i.qty}</span><button data-action="cart-quantity" data-index="${index}" data-delta="1" aria-label="Aumentar ${escapeHTML(item?.name||i.name)}" ${customerPending?'disabled':''}>+</button></div></div>`;}).join('')}<label class="field-label" for="customer-order-note">Observação geral (opcional)</label><textarea id="customer-order-note" maxlength="300" placeholder="Ex.: servir as entradas primeiro." ${customerPending?'disabled':''}>${escapeHTML(customerOrderNote)}</textarea><div class="total-line grand"><span>Valor dos itens</span><strong>${money(customerSubtotal())}</strong></div><p class="sheet-note">O serviço opcional é calculado na conta da mesa.</p><p id="customer-order-error" class="cart-error" role="alert">${escapeHTML(message)}</p><button class="primary-button" data-action="confirm-customer-order" ${customerSending||(!customerPending&&unavailable)?'disabled':''}>${customerSending?'Confirmando…':customerPending?'Confirmar novamente':'Confirmar pedido'} ${icon('check')}</button><button class="secondary-button" data-action="close-sheet" ${customerSending?'disabled':''}>Continuar escolhendo</button>`);
}
async function confirmCustomerOrder(){
  if(customerSending||!customerBasket.length)return;
  if(!customerPending){
    customerOrderNote=document.getElementById('customer-order-note').value.trim();
    for(const i of customerBasket){const item=menu.find(m=>m.id===i.menuId);if(!item){openCustomerCart('Um item ficou indisponível. Retire-o para continuar.');return;}i.reviewPrice=item.price;i.reviewName=item.name;}
    customerPending={key:crypto.randomUUID(),note:customerOrderNote,expectedSubtotal:customerSubtotal(),items:customerBasket.map(i=>({menuId:i.menuId,qty:i.qty,note:i.note}))};
    saveCustomerCart();
  }
  customerSending=true;openCustomerCart();
  try{
    if(serverMode){await api('/customer-orders',customerPending);await refreshLive();}
    else{for(const item of customerBasket){const dish=menu.find(m=>m.id===item.menuId);receiptItems.push({id:item.menuId,name:dish.name,price:dish.price,qty:item.qty});}saveDemoReceipt();}
    customerBasket=[];customerPending=null;customerOrderNote='';saveCustomerCart();closeSheet();
    if(location.hash==='#conta')await navigate('conta',true);else location.hash='conta';
    toast(serverMode?'Pedido confirmado. Já está na conta e na cozinha.':'Pedido demonstrativo adicionado à conta.');
  }catch(e){
    if([401,410].includes(e.status)){closeSheet();liveReady=false;renderAccess(e);return;}
    if([400,409].includes(e.status)){customerPending=null;saveCustomerCart();try{if(serverMode)await refreshLive();}catch{}}
    openCustomerCart(e.message);
  }finally{customerSending=false;const b=sheet.querySelector('[data-action="confirm-customer-order"]');if(b){b.disabled=!customerPending&&customerBasket.some(i=>!menu.some(m=>m.id===i.menuId));b.innerHTML=`${customerPending?'Confirmar novamente':'Confirmar pedido'} ${icon('check')}`;}renderCartBar();}
}
document.addEventListener('click',async event=>{
  const button=event.target.closest('[data-action]');if(!button)return;
  const action=button.dataset.action;
  if(action==='review-order'){openCustomerCart();return;}
  if(action==='dish-quantity'){const input=document.getElementById('dish-qty');input.value=Math.max(1,Math.min(100,Number(input.value)+Number(button.dataset.delta)));return;}
  if(action==='add-customer-item'){
    const dish=menu.find(m=>m.id===button.dataset.id),qty=Number(document.getElementById('dish-qty').value),note=document.getElementById('dish-note').value.trim();
    if(!dish||!Number.isInteger(qty)||qty<1||qty>100){toast('Confira a quantidade.');return;}
    const existing=customerBasket.find(i=>i.menuId===dish.id&&i.note===note);
    if((existing&&existing.qty+qty>100)||(!existing&&customerBasket.length>=40)){toast('Seu pedido já tem muitos itens. Confirme antes de adicionar mais.');return;}
    if(existing)existing.qty+=qty;else customerBasket.push({menuId:dish.id,name:dish.name,qty,note});saveCustomerCart();closeSheet();renderCartBar();toast('Adicionado ao seu pedido.');return;
  }
  if(action==='cart-quantity'&&!customerPending){const index=Number(button.dataset.index),item=customerBasket[index];if(!item)return;customerOrderNote=document.getElementById('customer-order-note').value;item.qty=Math.min(100,item.qty+Number(button.dataset.delta));if(item.qty<=0)customerBasket.splice(index,1);saveCustomerCart();openCustomerCart();renderCartBar();return;}
  if(action==='confirm-customer-order')await confirmCustomerOrder();
});
document.addEventListener('input',event=>{if(event.target.id==='customer-order-note'){customerOrderNote=event.target.value;saveCustomerCart();}});
