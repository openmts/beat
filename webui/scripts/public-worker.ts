export function publicWorker(version: string, paths: string[], shell: string): string {
 return `"use strict";
const VERSION=${JSON.stringify(version)}, CACHE='beat-public-'+VERSION, ALLOWED=new Set(${JSON.stringify(paths)}), SHELL=${JSON.stringify(shell)};
const safe=response=>response.ok&&!response.headers.has('Set-Cookie')&&!/no-store/i.test(response.headers.get('Cache-Control')||'');
const publicPage=url=>url.origin===self.location.origin&&url.pathname==='/';
self.addEventListener('install',event=>event.waitUntil((async()=>{
 const cache=await caches.open(CACHE);
 try { for(const path of ALLOWED){const response=await fetch(new Request(path,{credentials:'omit',cache:'reload'}));if(!safe(response))throw new Error('Unsafe public asset');await cache.put(path,response);} }
 catch(error){await caches.delete(CACHE);throw error;}
})()));
self.addEventListener('activate',event=>event.waitUntil((async()=>{
 const clients=await self.clients.matchAll({type:'window',includeUncontrolled:true});
 if(clients.length===0){const keys=await caches.keys();await Promise.all(keys.filter(key=>key.startsWith('beat-public-')&&key!==CACHE).map(key=>caches.delete(key)));}
})()));
self.addEventListener('message',event=>{
 if(event.data?.type==='BEAT_GET_VERSION'){event.source?.postMessage({type:'BEAT_VERSION',version:VERSION});return;}
 if(event.data?.type!=='BEAT_ACCEPT_UPDATE')return;
 event.waitUntil((async()=>{
  const clients=await self.clients.matchAll({type:'window',includeUncontrolled:true});
  const source=clients.find(client=>client.id===event.source?.id);
  if(!source||!publicPage(new URL(source.url))||clients.some(client=>!publicPage(new URL(client.url)))){event.source?.postMessage({type:'BEAT_UPDATE_DEFERRED',version:VERSION});return;}
  event.source.postMessage({type:'BEAT_UPDATE_ACCEPTED',version:VERSION});await self.skipWaiting();
 })());
});
self.addEventListener('fetch',event=>{
 const request=event.request,url=new URL(request.url);
 if(request.method!=='GET'||url.origin!==self.location.origin||request.headers.has('Authorization')||/no-store/i.test(request.headers.get('Cache-Control')||''))return;
 if(request.mode==='navigate'&&publicPage(url)){
  event.respondWith(fetch(request).catch(async()=>{const cached=await(await caches.open(CACHE)).match(SHELL);return cached||new Response('Offline',{status:503,headers:{'Content-Type':'text/plain'}});}));return;
 }
 if(!ALLOWED.has(url.pathname+url.search))return;
 event.respondWith((async()=>{const cache=await caches.open(CACHE),cached=await cache.match(url.pathname+url.search);if(cached)return cached;const response=await fetch(request);if(safe(response))await cache.put(url.pathname+url.search,response.clone());return response;})());
});`
}
