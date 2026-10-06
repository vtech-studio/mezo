'use strict';
// The static publication remains an explicitly identified demo. Go inserts the runtime marker.
async function api(path,body){
  const res=await fetch(`/api/r/${encodeURIComponent(tenantSlug)}${path}`,{method:body===undefined?'GET':'POST',headers:body===undefined?{}:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:body===undefined?undefined:JSON.stringify(body),credentials:'same-origin'});
  const data=await res.json();if(!res.ok){const error=new Error(data.error||'Não foi possível concluir.');error.status=res.status;throw error;}return data;
}
async function refreshLive(){
  const data=await api(`/bootstrap?role=${role==='waiter'?'staff':'customer'}`);
  csrf=data.csrf;restaurantName=data.restaurant.name;menu=data.menu;state={calls:data.calls,service:data.service??true};
  liveTransport=data.realtime||'events';
  if(data.table){tableNumber=data.table.number;tableSeats=data.table.seats;receiptItems=data.items;liveTotals=data.totals;}
  if(data.staff){staffName=data.staff.name;staffID=data.staff.id;tableData=data.tables;}
  document.querySelector('.restaurant>span:last-child').innerHTML=`${escapeHTML(restaurantName)}<small>À SUA MESA COM MEZÔ</small>`;
  document.querySelector('.restaurant').setAttribute('aria-label',`${restaurantName}, início`);
  document.title=`Mezô · ${restaurantName}`;
}
function connectLive(){
  clearInterval(livePollTimer);
  if(eventStream)eventStream.close();
  const currentRole=role;
  if(liveTransport==='poll'){
    let pending=false;
    livePollTimer=setInterval(async()=>{
      if(pending||role!==currentRole||document.hidden||!liveReady)return;
      pending=true;
      try{await refreshLive();if(!sheet.open&&document.activeElement?.id!=='menu-search')render();else renderNav();}
      catch(e){if([401,410].includes(e.status)){clearInterval(livePollTimer);liveReady=false;closeSheet();renderAccess(e);}else document.querySelector('.live-label').textContent='Reconectando…';}
      finally{pending=false;}
    },5000);
    return;
  }
  eventStream=new EventSource(`/api/r/${encodeURIComponent(tenantSlug)}/events?role=${role==='waiter'?'staff':'customer'}`);
  eventStream.addEventListener('update',async()=>{
    if(role!==currentRole)return;
    try{await refreshLive();if(!sheet.open&&document.activeElement?.id!=='menu-search')render();else renderNav();}
    catch(e){eventStream.close();liveReady=false;closeSheet();renderAccess(e);}
  });
  eventStream.addEventListener('expired',()=>{eventStream.close();liveReady=false;closeSheet();renderAccess(new Error('Sua sessão terminou. Entre novamente ou escaneie a placa da mesa.'));});
  eventStream.onerror=()=>{document.querySelector('.live-label').textContent='Reconectando…';};
  eventStream.onopen=()=>{if(liveReady)renderNav();};
}
function renderAccess(error){
  renderNav();
  if(role==='waiter'){
    main.innerHTML=`<div class="page-heading"><p class="eyebrow">MEZÔ · ACESSO DA EQUIPE</p><h1>Vamos cuidar<br>da casa.</h1><p>Entre com o acesso deste restaurante.</p></div><form id="staff-login"><label class="field-label" for="staff-email">E-mail</label><input class="auth-input" id="staff-email" type="email" autocomplete="username" required><label class="field-label" for="staff-password">Senha</label><input class="auth-input" id="staff-password" type="password" autocomplete="current-password" required><button class="primary-button" type="submit" data-action="login">Entrar ${icon('arrow')}</button><p class="sheet-note" id="login-error" role="alert"></p></form><footer class="hospitality-note"><span class="powered">mezô</span></footer>`;
    main.querySelector('form').addEventListener('submit',async e=>{e.preventDefault();await liveAction('login',main.querySelector('[data-action="login"]'));});
  }else{
    main.innerHTML=`<div class="empty-state">${icon('table')}<h3>Seu lugar está na mesa.</h3><p>${escapeHTML(error.status===401?'Escaneie o QR code ou aproxime o celular da placa para entrar.':error.message)}</p><a class="secondary-button" href="/">Ir para o início</a></div>`;
  }
}
async function sendLiveCall(){
  const button=sheet.querySelector('[data-action="send-call"]');button.disabled=true;
  try{
    const result=await api('/calls',{reason:selectedReason,note:sheet.querySelector('#call-note').value.trim()});await refreshLive();render();
    openSheet(result.duplicate?'O chamado está na fila':'Chamado enviado',`<div class="success-body"><div class="success-illustration">${icon('check')}</div><h2>Já avisamos a equipe.</h2><p>Mesa ${tableName(tableNumber)} · seu atendimento acompanha esta mesa.</p></div><button class="primary-button" data-action="close-sheet">Tudo certo ${icon('check')}</button>`);
  }catch(e){toast(e.message);button.disabled=false;}
}
function openLiveTable(n){
  const table=tableData.find(t=>t.number===n);if(!table)return;
  const calls=state.calls.filter(c=>c.sessionId===table.sessionId&&['pending','progress'].includes(c.status));
  openSheet(`Mesa ${tableName(n)}`,`<p class="sheet-description">${table.sessionId?`Mesa aberta · consumo ${money(table.subtotal)}`:'Mesa fechada · pronta para receber'}</p>${calls.length?`<div class="call-list">${calls.map(waiterCard).join('')}</div>`:''}${table.sessionId?`<h3>Registrar pedido confirmado</h3>${menu.length?`<label class="field-label" for="consumption-item">Item do cardápio</label><select id="consumption-item" class="auth-input">${menu.map(d=>`<option value="${escapeHTML(d.id)}">${escapeHTML(d.name)} · ${money(d.price)}</option>`).join('')}</select><label class="field-label" for="consumption-qty">Quantidade</label><input id="consumption-qty" class="auth-input" type="number" min="1" max="100" value="1"><button class="primary-button" data-action="consumption" data-table="${n}">Registrar consumo</button>`:'<p class="sheet-description">O administrador precisa cadastrar o cardápio.</p>'}<button class="secondary-button" data-action="close-table" data-table="${n}">Encerrar conta presencial</button><p class="sheet-note">Confirme o consumo com o cliente. A comunicação com a cozinha continua presencial.</p>`:`<button class="primary-button" data-action="open-table" data-table="${n}">Abrir mesa</button>`}`);
}
async function liveAction(action,target){
  if(action==='close-table'){openSheet('Encerrar esta conta?',`<p class="sheet-description">Confirme que o pagamento foi recebido presencialmente. Este botão encerra o atendimento da mesa; ele não realiza cobrança.</p><button class="primary-button" data-action="confirm-close" data-table="${Number(target.dataset.table)}">Confirmar encerramento</button><button class="secondary-button" data-action="close-sheet">Voltar</button>`);return;}
  target.disabled=true;
  try{
    if(action==='login'){
      const email=document.querySelector('#staff-email').value,password=document.querySelector('#staff-password').value;
      if(!email||!password){document.querySelector('#login-error').textContent='Preencha e-mail e senha.';return;}
      await api('/login',{email,password});await navigate('fila');return;
    }
    if(action==='logout'){await api('/logout',{});closeSheet();liveReady=false;if(eventStream)eventStream.close();clearInterval(livePollTimer);renderAccess(new Error('Entre para continuar'));return;}
    const n=Number(target.dataset.table);
    if(action==='consumption'){await api(`/tables/${n}/consumption`,{menuId:sheet.querySelector('#consumption-item').value,qty:Number(sheet.querySelector('#consumption-qty').value)});await refreshLive();openLiveTable(n);toast('Consumo registrado na conta.');return;}
    if(action==='confirm-close')await api(`/tables/${n}/close`,{confirmed:true});
    if(action==='open-table')await api(`/tables/${n}/open`,{});
    await refreshLive();closeSheet();render();toast(action==='open-table'?'Mesa aberta.':'Conta encerrada.');
  }catch(e){if(action==='login')document.querySelector('#login-error').textContent=e.message;else toast(e.message);}
  finally{target.disabled=false;}
}
