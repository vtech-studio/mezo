'use strict';
// Sound, vibration and a screen that stays on for the staff screens. Browsers only
// allow sound after a touch on the page, so the choice is remembered and the audio
// resumes on the next touch after a reload.
window.MezoAlerts=(()=>{
 const key='mezo-alerts';
 let audio=null,lock=null,known=null,enabled=false;
 try{enabled=localStorage.getItem(key)==='on';}catch{}
 async function keepAwake(){
  if(!enabled||document.hidden||lock||!('wakeLock' in navigator))return;
  try{lock=await navigator.wakeLock.request('screen');lock.addEventListener('release',()=>{lock=null;});}catch{}
 }
 function unlock(){
  if(!enabled)return;
  try{audio=audio||new (window.AudioContext||window.webkitAudioContext)();if(audio.state==='suspended')audio.resume().catch(()=>{});}catch{}
  keepAwake();
 }
 function tone(){
  if(audio?.state!=='running')return;
  const now=audio.currentTime;
  for(const [frequency,start] of [[880,0],[1320,0.22]]){
   const oscillator=audio.createOscillator(),gain=audio.createGain();
   oscillator.frequency.value=frequency;
   gain.gain.setValueAtTime(0.0001,now+start);gain.gain.exponentialRampToValueAtTime(0.6,now+start+0.02);gain.gain.exponentialRampToValueAtTime(0.0001,now+start+0.18);
   oscillator.connect(gain).connect(audio.destination);oscillator.start(now+start);oscillator.stop(now+start+0.2);
  }
 }
 function signal(){tone();try{navigator.vibrate?.([200,100,200]);}catch{}}
 // Receives everything that currently needs someone and returns what is new since
 // the previous update. The first update only records what is already there.
 function arrivals(items){
  const fresh=known?items.filter(item=>!known.has(item.key)):[];
  known=new Set(items.map(item=>item.key));
  if(fresh.length&&enabled)signal();
  return fresh;
 }
 function set(value){
  enabled=value;
  try{localStorage.setItem(key,value?'on':'off');}catch{}
  if(value){unlock();Promise.resolve(audio?.resume()).then(signal).catch(()=>{});}
  else if(lock){lock.release().catch(()=>{});lock=null;}
 }
 document.addEventListener('pointerdown',unlock);document.addEventListener('keydown',unlock);
 document.addEventListener('visibilitychange',()=>{if(!document.hidden)keepAwake();});
 keepAwake();
 return {arrivals,set,reset(){known=null;},get enabled(){return enabled;}};
})();
