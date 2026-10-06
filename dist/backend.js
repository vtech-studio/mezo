'use strict';
// The static publication remains an explicitly identified demo. Go inserts the runtime marker.
async function api(path,body){
  const controller=new AbortController(),timeout=setTimeout(()=>controller.abort(),15000);
  try{
    const res=await fetch(`/api/r/${encodeURIComponent(tenantSlug)}${path}`,{method:body===undefined?'GET':'POST',headers:body===undefined?{}:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:body===undefined?undefined:JSON.stringify(body),credentials:'same-origin',signal:controller.signal});
    let data;try{data=await res.json();}catch{throw new Error('Resposta indisponível. Confira a conexão e tente novamente.');}
    if(!res.ok){const error=new Error(data.error||'Não foi possível concluir.');error.status=res.status;throw error;}return data;
  }catch(e){if(e.name==='AbortError'||e instanceof TypeError)throw new Error('Não foi possível confirmar o envio. Confira a conexão e confirme novamente; o pedido será lançado uma única vez.');throw e;}finally{clearTimeout(timeout);}
}
async function refreshLive(){
  const data=await api('/bootstrap');
  csrf=data.csrf;restaurantName=data.restaurant.name;menu=data.menu;state={calls:data.calls,service:data.service??true};
  liveOrders=data.orders||[];liveSessionId=data.sessionId||'';
  liveTransport=data.realtime||'events';
  if(data.table){tableNumber=data.table.number;tableSeats=data.table.seats;receiptItems=data.items;liveTotals=data.totals;}
  document.querySelector('.restaurant>span:last-child').innerHTML=`${escapeHTML(restaurantName)}<small>À SUA MESA</small>`;
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
  eventStream=new EventSource(`/api/r/${encodeURIComponent(tenantSlug)}/events?role=customer`);
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
  main.innerHTML=`<div class="empty-state">${icon('table')}<h3>Seu lugar está na mesa.</h3><p>${escapeHTML(error.status===401?'Escaneie o QR code ou aproxime o celular da placa para entrar.':error.message)}</p><a class="secondary-button" href="/">Ir para o início</a></div>`;
}
async function sendLiveCall(){
  const button=sheet.querySelector('[data-action="send-call"]');button.disabled=true;
  try{
    const result=await api('/calls',{reason:selectedReason,note:sheet.querySelector('#call-note').value.trim()});await refreshLive();render();
    openSheet(result.duplicate?'O chamado está na fila':'Chamado enviado',`<div class="success-body"><div class="success-illustration">${icon('check')}</div><h2>Já avisamos a equipe.</h2><p>Mesa ${tableName(tableNumber)} · seu atendimento acompanha esta mesa.</p></div><button class="primary-button" data-action="close-sheet">Tudo certo ${icon('check')}</button>`);
  }catch(e){toast(e.message);button.disabled=false;}
}
