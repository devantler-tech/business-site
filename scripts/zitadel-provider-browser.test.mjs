// Unit controls for the exact scripts executed by the native browser worker.
// Synthetic inputs here are not native rendering, federation or activation proof.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const source=readFileSync(new URL('./zitadel-browser.go',import.meta.url),'utf8');
const script=name=>{const match=source.match(new RegExp('const '+name+' = `([\\s\\S]*?)`'));assert.ok(match,name);return new Function(match[1]);};
// The exact native observation must prove a visible, focused, unconditional
// source offer. These synthetic controls are not actual browser rendering.
const sourceOfferObservation=script('browserSourceOfferObservation');
const offerURL='https://github.com/devantler-tech/business-site/releases/tag/identity-v4.20.0-devantler.1';
for(const kind of ['console','login'])for(const fault of ['none','missing','duplicate','wrong-url','query','hidden','covered','offscreen','invisible','unfocused','negative-tabindex','wrong-name','missing-rel','wrong-target','opposite-theme',...(kind==='console'?['transitioning-theme','missing-theme-root','wrong-background']:[])]){
 const a={tagName:'A',href:offerURL+(fault==='query'?'?latest=1':''),innerText:fault==='wrong-name'?'Help':'Source code',tabIndex:fault==='negative-tabindex'?-1:0,
  rel:fault==='missing-rel'?'noopener':'noopener noreferrer',target:fault==='wrong-target'?'_self':'_blank',
  getAttribute:name=>name==='href'?(fault==='wrong-url'?'https://outside.invalid/source':a.href):null,
  getClientRects:()=>fault==='hidden'?[]:[{}],closest:()=>fault==='hidden'?{}:null,
  getBoundingClientRect:()=>({x:20,y:20,left:20,right:120,top:20,bottom:fault==='offscreen'?900:40,width:100,height:20})};
 const back={getAnimations:()=>fault==='transitioning-theme'?[{pending:false,playState:'running'}]:[]};
 globalThis.location=new URL('http://fixture.localhost:8080/ui/login/login');
 globalThis.innerWidth=390;globalThis.innerHeight=844;
 globalThis.getComputedStyle=e=>e===back?{backgroundColor:fault==='wrong-background'?'rgb(17, 24, 39)':'rgb(250, 250, 250)'}:{visibility:fault==='invisible'?'hidden':'visible',display:'inline',opacity:'1'};
 globalThis.document={activeElement:fault==='unfocused'?{}:a,querySelectorAll:()=>fault==='missing'?[]:fault==='duplicate'?[a,a]:[a],elementFromPoint:()=>({closest:()=>fault==='covered'?{}:a}),
  documentElement:{classList:{contains:name=>name==='lgn-light-theme'&&fault!=='opposite-theme'}},
  querySelector:selector=>selector==='cnsl-root.light-theme'?(fault==='opposite-theme'||fault==='missing-theme-root'?null:{}):selector==='cnsl-root .main-container'?back:null};
 const p=sourceOfferObservation(kind,'light');
 assert.equal(typeof p.themeReady,'boolean','source-offer theme observation is missing');
 assert.equal(p.visible&&p.keyboard&&p.themeReady,fault==='none','source offer admitted '+kind+'/'+fault);
 if(fault==='none')assert.equal(p.href,offerURL);
}
console.log('PASS: exact source-offer observation in both interfaces and 31 adverse visibility, focus and settled-theme controls');
const observe=script('browserProviderObservation');
globalThis.location=new URL('http://fixture.localhost:8080/ui/login/login');
globalThis.document={querySelectorAll:()=>[],querySelector:()=>null,documentElement:{classList:{contains:()=>true}},readyState:'complete',fonts:{status:'loaded'}};
for(const resource of ['http://fixture.localhost:8080/ui/login/resources/images/idp/google.png','http://fixture.localhost:8080/ui/console/assets/images/idp/apple.svg','https://outside.invalid/font.woff2']){
 globalThis.performance={getEntriesByType:()=>[{name:resource}]};
 assert.equal(observe('login','light').resources,false,'retired/external native resource admitted');
}
globalThis.performance={getEntriesByType:()=>[{name:'http://fixture.localhost:8080/ui/login/resources/fonts/lgn-icons.woff'}]};
assert.equal(observe('login','light').resources,true,'ordinary local resource refused');
// Execute the same page observation on the native settings-frame shape: it has
// a header and settings component, but intentionally has no home-navigation bar.
const frameObservation=script('browserObservation');
const visible={getClientRects:()=>[{}]};
globalThis.window={isSecureContext:true};
globalThis.getComputedStyle=()=>({fontFamily:'Roboto, sans-serif'});
const fonts=[{family:'Roboto',status:'loaded'}];fonts.status='loaded';
for(const mode of ['native-settings','missing-header','missing-frame']){
 const root={querySelector:selector=>selector==='cnsl-header'&&mode!=='missing-header'?visible:null};
 globalThis.document={body:{innerText:'Instance Settings Identity Providers Fixture Google Fixture GitHub'},fonts,readyState:'complete',
  querySelector:selector=>selector==='cnsl-root'?root:null,
  querySelectorAll:selector=>selector==='cnsl-header'&&mode!=='missing-header'||selector==='cnsl-instance,cnsl-provider-google,cnsl-provider-github,cnsl-provider-oidc,cnsl-provider-jwt'&&mode!=='missing-frame'?[visible]:[]};
 const page=frameObservation();
 assert.equal(page.providerConsole,mode==='native-settings',mode+' gave wrong provider-frame observation');
 assert.equal(page.console,false,'settings frame satisfied home console requirement');
 assert.equal(page.consolePresented,false,'settings frame satisfied home navigation requirement');
}
const registrationURL=script('browserRegistrationURL');
globalThis.location=new URL('http://fixture.localhost:8080/ui/login/login');
for(const id of ['fictional-request_01','',null,'bad/request','x'.repeat(129)]){
 globalThis.document={querySelector:selector=>{assert.equal(selector,'input[name="authRequestID"]');return id===null?null:{value:id};}};
 const value=registrationURL();
 if(id==='fictional-request_01'){
  const url=new URL(value);
  assert.equal(url.origin,location.origin);assert.equal(url.pathname,'/ui/login/register/option');
  assert.deepEqual([...url.searchParams],[['authRequestID',id]],'wrong native auth-request parameter');
 }else assert.equal(value,'','invalid or absent auth request admitted');
}
const setup=script('browserProviderSetup');
for(const mode of ['positive','bad-duration','bad-boolean','http-error','malformed-body','missing-provider','wrong-policy','wrong-name','duplicate-id','wrong-origin']){
 const ids=['123456','789012'];const bools=['allowUsernamePassword','allowRegister','allowExternalIdp','forceMfa','hidePasswordReset','ignoreUnknownUsernames','allowDomainDiscovery','disableLoginWithEmail','disableLoginWithPhone','forceMfaLocalOnly'];
 const durations=['passwordCheckLifetime','externalLoginCheckLifetime','mfaInitSkipLifetime','secondFactorCheckLifetime','multiFactorCheckLifetime'];
 let policy=Object.fromEntries(bools.map((key,i)=>[key,i>2&&i%2===0]));policy.allowUsernamePassword=true;
 for(const [i,key]of durations.entries())policy[key]=`${i+10}s`;
 Object.assign(policy,{defaultRedirectUri:'',passwordlessType:'PASSWORDLESS_TYPE_ALLOWED',secondFactors:[1],multiFactors:[2],isDefault:false});
 if(mode==='bad-duration')policy.passwordCheckLifetime='1 hour';if(mode==='bad-boolean')policy.forceMfa='false';
 let put=false,created=0;const linked=[];const requests=[];
 globalThis.location=new URL(mode==='wrong-origin'?'https://outside.invalid/ui/console/':'http://fixture.localhost:8080/ui/console/');
 globalThis.sessionStorage={getItem:key=>{assert.equal(key,'zitadel:access_token');return JSON.stringify('fictional-unit-bearer');}};
 globalThis.fetch=async(path,options)=>{
  requests.push(path);assert.ok(path.startsWith('/admin/v1/'));assert.equal(options.redirect,'error');assert.equal(options.cache,'no-store');assert.equal(options.headers.Authorization,'Bearer fictional-unit-bearer');
  const body=options.body===undefined?undefined:JSON.parse(options.body);let response={details:{}};
  if(path==='/admin/v1/policies/login'&&options.method==='GET'){response={policy:structuredClone(policy)};if(mode==='wrong-policy'&&put)response.policy.forceMfa=!response.policy.forceMfa;response.policy.idps=linked.map((idpId,i)=>({idpId,idpName:['Fixture Google','Fixture GitHub'][i]}));}
  else if(path==='/admin/v1/policies/login'&&options.method==='PUT'){
   assert.deepEqual(Object.keys(body).sort(),[...bools,...durations,'defaultRedirectUri','passwordlessType'].sort());
   const expected=structuredClone(policy);for(const key of ['secondFactors','multiFactors','isDefault'])delete expected[key];expected.allowExternalIdp=true;assert.deepEqual(body,expected);
   policy={...policy,...body};put=true;
  }else if(path==='/admin/v1/idps/google'||path==='/admin/v1/idps/github'){
   assert.equal(body.providerOptions.isCreationAllowed,false);assert.equal(body.providerOptions.isLinkingAllowed,false);
   response={details:{},id:mode==='duplicate-id'?ids[0]:ids[created]};created++;
  }else if(path.startsWith('/admin/v1/idps/templates/')){
   const id=path.split('/').at(-1);response={idp:{id,name:mode==='wrong-name'?'Wrong':id===ids[0]?'Fixture Google':'Fixture GitHub'}};
  }else if(path==='/admin/v1/policies/login/idps'){assert.ok(ids.includes(body.idpId));linked.push(body.idpId);}
  else if(path==='/admin/v1/policies/login/idps/_search')response={details:{totalResult:'2'},result:linked.slice(0,mode==='missing-provider'?1:2).map((idpId,i)=>({idpId,idpName:['Fixture Google','Fixture GitHub'][i]}))};
  else assert.fail('unexpected API request');
  return {ok:mode!=='http-error',status:mode==='http-error'?403:200,headers:{get:()=> 'application/json'},text:async()=>mode==='malformed-body'?'incomplete JSON':JSON.stringify(response)};
 };
 const result=await new Promise(resolve=>setup(resolve));
 assert.equal(result.complete,mode==='positive',`${mode} returned wrong verdict`);
 if(mode==='positive'){assert.equal(result.count,2);assert.equal(result.phase,4);assert.equal(result.request,11);assert.equal(result.status,200);assert.deepEqual(result.ids,ids);assert.equal(requests.length,11);}
 else{assert.equal(result.count,0);assert.equal(JSON.stringify(result).includes('fictional-unit-bearer'),false);}
}
console.log('PASS: actual observation-script resource controls and ten closed fixture API cases (unit inputs only)');

// SMTP rendering is admitted independently of the existing ten IdP cases.
const smtpObserve=script('browserSMTPObservation');
for(const kind of ['picker','amazon','google','mailgun','mailjet','postmark','sendgrid','mailchimp','brevo','outlook']){
 for(const fault of ['none','missing-label','bad-host','bad-auth','hidden','focusable-decoration','wrong-glyph','external-resource','retired-resource','misassociated-glyph','missing-theme','missing-page-background','wrong-page-background','animating-page-background','pending-page-background']){
  const names=['amazon','brevo','google','mailchimp','mailgun','mailjet','outlook','postmark','sendgrid'];
  const labels=['Amazon Ses','Brevo','Google','Mailchimp','Mailgun','Mailjet','Microsoft Exchange Online','Postmark','Sendgrid'];
  const host={amazon:'email-smtp.us-east-2.amazonaws.com:587',google:'smtp.gmail.com:587',mailgun:'smtp.mailgun.org:465',mailjet:'in-v3.mailjet.com:465',postmark:'smtp.postmarkapp.com:587',sendgrid:'smtp.sendgrid.net:465',mailchimp:'smtp.mandrillapp.com:465',brevo:'smtp-relay.sendinblue.com:465',outlook:'smtp.office365.com:587'};
  const description={amazon:'amazon SES',google:'google',mailgun:'mailgun',mailjet:'mailjet',postmark:'postmark',sendgrid:'sendgrid',mailchimp:'mailchimp',brevo:'brevo',outlook:'Microsoft Exchange Online'};
  const rect={x:1,y:1,left:1,top:1,right:81,bottom:81,width:80,height:80};
  const shown={getBoundingClientRect:()=>rect,getClientRects:()=>fault==='hidden'?[]:[rect]};
  const anchors=names.map((name,i)=>({...shown,origin:'http://fixture.localhost:8080',pathname:'/ui/console/instance/smtpprovider/'+name,tabIndex:0,innerText:fault==='missing-label'?'':labels[i]}));
  anchors.push({...shown,origin:'http://fixture.localhost:8080',pathname:'/ui/console/instance/smtpprovider/generic',tabIndex:0,innerText:'Generic SMTP'});
  const icon=name=>({...shown,tabIndex:fault==='focusable-decoration'?0:-1,getAttribute:key=>key==='aria-hidden'?'true':null,closest:selector=>selector==='a.item'?anchors[fault==='misassociated-glyph'?0:names.indexOf(name)]:null});
  const icons=(kind==='picker'?names:[kind]).map(icon);
  globalThis.location=new URL('http://fixture.localhost:8080/ui/console/instance'+(kind==='picker'?'?id=smtpprovider':'/smtpprovider/'+kind));
  globalThis.innerHeight=1080;globalThis.innerWidth=1440;
  const background={getAnimations:()=>fault==='animating-page-background'?[{playState:'running',pending:false}]:fault==='pending-page-background'?[{playState:'finished',pending:true}]:[]};
  globalThis.getComputedStyle=(e,pseudo)=>e===background?{backgroundColor:fault==='wrong-page-background'?'rgb(17, 24, 39)':'rgb(250, 250, 250)'}:pseudo?{content:'"'+String.fromCodePoint(fault==='wrong-glyph'?0xf061:0xf0e0)+'"',fontFamily:'"Line Awesome Free"'}:{fontFamily:'"Line Awesome Free"',fontSize:(kind==='picker'?36:48)+'px'};
  const resource=fault==='external-resource'?'https://outside.invalid/font.woff2':fault==='retired-resource'?'http://fixture.localhost:8080/ui/console/assets/images/smtp/google.png':'http://fixture.localhost:8080/ui/console/assets/fonts/la-solid-900.woff2';
  globalThis.performance={getEntriesByType:()=>[{name:resource}]};
  const fonts=[{family:'Line Awesome Free',status:'loaded'}];fonts.status='loaded';
  const inputs={description:{value:description[kind]},hostAndPort:{value:fault==='bad-host'?'incorrect':host[kind]},'smtp-user':{value:kind==='sendgrid'?'apikey':''},scopes:{value:'https://outlook.office.com/SMTP.Send'}};
  globalThis.document={readyState:'complete',fonts,body:{innerText:labels[names.indexOf(kind)]??'SMTP providers'},
   querySelectorAll:selector=>selector.includes('.neutral-smtp-icon')?icons:selector==='cnsl-notification-smtp-provider a.item'?anchors:[],
   querySelector:selector=>{
    if(selector==='cnsl-root.light-theme')return fault==='missing-theme'?null:{};
    if(selector==='cnsl-root .main-container')return fault==='missing-page-background'?null:background;
    if(selector==='cnsl-smtp-provider input[name="smtp-password"]')return kind==='outlook'?null:{};
    if(selector==='cnsl-smtp-provider input[name="token-endpoint"]')return kind==='outlook'?{}:null;
    if(selector==='cnsl-smtp-provider mat-checkbox.smtp-checkbox:nth-of-type(2) input')return {checked:fault==='bad-auth'?kind!=='outlook':kind==='outlook',disabled:true};
    const match=selector.match(/^cnsl-smtp-provider input\[name="([^"]+)"\]$/);if(match)return inputs[match[1]]??null;
    return null;
   }};
  const result=smtpObserve(kind,'light');
  const themeComplete=!['missing-theme','missing-page-background','wrong-page-background','animating-page-background','pending-page-background'].includes(fault);
  assert.equal(result.themeReady,themeComplete,kind+'/'+fault+' SMTP theme');
  assert.equal(observe('picker','light').themeReady,themeComplete,kind+'/'+fault+' IdP theme');
  const relevant=fault==='none'||kind==='picker'&&['bad-host','bad-auth'].includes(fault)||kind!=='picker'&&['missing-label','misassociated-glyph'].includes(fault);
  assert.equal(result.glyphs&&result.labels&&result.targets&&result.themeReady&&result.resources&&result.keyboard,relevant,kind+'/'+fault);
 }
}
console.log('PASS: exact native SMTP observation script - 150 independent rendering, settled-theme and default controls (unit inputs only)');

// Execute the actual MDI worker observation with adverse native-shaped inputs.
const mdiObserve=script('browserMDIObservation');
for(const kind of ['mdi-picker','mdi-table','editor-oidc','editor-jwt']){
 for(const fault of ['none','hidden','focusable-decoration','wrong-glyph','misassociated-glyph','missing-label','missing-control','external-resource','retired-resource','unloaded-font','missing-theme','animating-background']){
  const picker=kind==='mdi-picker',table=kind==='mdi-table';
  const types=picker||table?['oidc','jwt']:[kind.slice(7)];
  const rect={left:1,top:1,right:100,bottom:100,width:99,height:99};
  const visible={getBoundingClientRect:()=>rect,getClientRects:()=>fault==='hidden'?[]:[rect]};
  const anchors=types.map(t=>({...visible,origin:location.origin,pathname:'/ui/console/instance/provider/'+t+'/create',innerText:fault==='missing-label'?'':t.toUpperCase(),tabIndex:0}));
  const rows=types.map(t=>({...visible,innerText:fault==='missing-label'?'':'Fixture '+t.toUpperCase(),querySelector:()=>fault==='missing-control'?null:{}}));
  const icons=types.map((t,i)=>({...visible,type:fault==='misassociated-glyph'?(t==='jwt'?'oidc':'jwt'):t,tabIndex:fault==='focusable-decoration'?0:-1,classList:{contains:c=>c===((fault==='misassociated-glyph'?t!=='jwt':t==='jwt')?'la-key':'la-arrow-right')},getAttribute:()=> 'true',closest:s=>s==='tr'?rows[i]:anchors[i]}));
  // Native settings contains the configured-provider table as well as the chooser.
  // Its two sibling table icons must never enter the chooser observation.
  const tableIcons=picker?icons.map((e,i)=>({...e,tableOnly:true,closest:s=>s==='tr'?rows[i]:null})):[];
  const background={getAnimations:()=>fault==='animating-background'?[{pending:false,playState:'running'}]:[]};
  globalThis.location=new URL('http://fixture.localhost:8080'+(picker||table?'/ui/console/instance?id=idp':'/ui/console/instance/provider/'+kind.slice(7)+'/create'));
  const fonts=[{family:'Line Awesome Free',status:fault==='unloaded-font'?'error':'loaded'}];fonts.status='loaded';
  globalThis.document={readyState:'complete',fonts,body:{innerText:fault==='missing-label'?'':kind.slice(7).toUpperCase()},
   querySelectorAll:s=>!s.includes('.neutral-mdi-icon')?[]:picker&&s==='cnsl-idp-settings .neutral-mdi-icon'?[...icons,...tableIcons]:icons,querySelector:s=>s==='cnsl-root.light-theme'?(fault==='missing-theme'?null:{}):s==='cnsl-root .main-container'?background:fault==='missing-control'?null:{}};
  globalThis.getComputedStyle=(e,pseudo)=>e===background?{backgroundColor:'rgb(250, 250, 250)'}:pseudo?{content:'"'+String.fromCodePoint(fault==='wrong-glyph'?0xf0e0:e.type==='jwt'?0xf084:0xf061)+'"',fontFamily:'Line Awesome Free'}:{fontSize:(e.tableOnly?28:picker?36:table?28:48)+'px'};
  const resource=fault==='external-resource'?'https://outside.invalid/font.woff2':fault==='retired-resource'?'http://fixture.localhost:8080/ui/console/assets/mdi/openid.svg':'http://fixture.localhost:8080/ui/console/assets/fonts/la-solid-900.woff2';
  globalThis.performance={getEntriesByType:()=>[{name:resource}]};
  const result=mdiObserve(kind,'light');
  const relevant=fault==='none'||picker&&fault==='missing-control';
  assert.equal(result.glyphs&&result.labels&&result.targets&&result.themeReady&&result.resources&&result.keyboard,relevant,kind+'/'+fault+' '+JSON.stringify(result));
 }
}
console.log('PASS: actual MDI observation script - 48 independent native-shaped controls (unit inputs only)');

// The exact setup script creates only unlinked fictional protocol fixtures.
const mdiSetup=script('browserMDISetup');
for(const fault of ['none','wrong-origin','http-error','malformed-json','bad-id','duplicate-id','wrong-name','changed-policy','missing-linked-provider']){
 globalThis.location=new URL(fault==='wrong-origin'?'https://outside.invalid/':'http://fixture.localhost:8080/ui/console/instance?id=idp');
 globalThis.sessionStorage={getItem:()=>JSON.stringify('fictional-native-fixture-token')};
 let creates=0,calls=0;
 const linked=[{idpId:'101',idpName:'Fixture Google'},{idpId:'102',idpName:'Fixture GitHub'}];
 globalThis.fetch=async(path,options)=>{
  calls++;assert.equal(options.redirect,'error');assert.equal(options.cache,'no-store');assert.equal(options.headers.Authorization,'Bearer fictional-native-fixture-token');
  let body;
  if(path==='/admin/v1/policies/login'){
   assert.equal(options.method,'GET','MDI fixture mutated login policy');
   body={policy:{allowRegister:false,allowExternalIdp:true,idps:fault==='missing-linked-provider'?linked.slice(0,1):linked}};
   if(fault==='changed-policy'&&creates===2)body.policy.allowRegister=true;
  }else if(path==='/admin/v1/idps/generic_oidc'||path==='/admin/v1/idps/generic_jwt'){
   assert.equal(options.method,'POST');creates++;const posted=JSON.parse(options.body);
   assert.equal(posted.providerOptions.isCreationAllowed,false);assert.equal(posted.providerOptions.isLinkingAllowed,false);
   assert.equal(posted.issuer,location.origin);
   if(creates===1){assert.equal(posted.name,'Fixture OIDC');assert.equal(posted.clientId,'fictional-oidc-client');}
   else{assert.equal(posted.name,'Fixture JWT');assert.equal(posted.jwtEndpoint,location.origin+'/fictional-jwt');assert.equal(posted.keysEndpoint,location.origin+'/oauth/v2/keys');}
   body={id:fault==='bad-id'?'bad/id':fault==='duplicate-id'?'201':String(200+creates)};
  }else{
   assert.equal(options.method,'GET');assert.ok(['/admin/v1/idps/templates/201','/admin/v1/idps/templates/202'].includes(path));
   body={idp:{id:path.split('/').at(-1),name:fault==='wrong-name'?'Wrong':creates===1?'Fixture OIDC':'Fixture JWT'}};
  }
  return {ok:fault!=='http-error',status:fault==='http-error'?500:200,headers:{get:()=> 'application/json'},text:async()=>fault==='malformed-json'?'not-json':JSON.stringify(body)};
 };
 const result=await new Promise(resolve=>mdiSetup(resolve));
 assert.equal(result.complete,fault==='none',fault+' MDI setup');
 if(fault==='none'){assert.equal(result.count,2);assert.deepEqual(result.ids,['201','202']);assert.equal(calls,6);}
 if(fault==='wrong-origin')assert.equal(calls,0);
}
console.log('PASS: actual MDI setup script - nine same-origin, unchanged-policy and provider readback controls (unit inputs only)');

// These are controls for the exact native-page observer, not real browser visits.
const frameworkObserve=script('browserFrameworkObservation');
for(const kind of ['framework-quickstart','framework-selected','framework-picker']){
 for(const fault of ['none','wrong-id','wrong-glyph','missing-label','missing-control','missing-theme','unloaded-font','retired-resource','external-resource','focusable-decoration','running-descendant','pending-descendant','perpetual-decoration']){
  const quick=kind==='framework-quickstart',selected=kind==='framework-selected';
  const shown={getClientRects:()=>[{}],getBoundingClientRect:()=>({width:50,height:50,top:10,left:10,bottom:60,right:60})};
  const background={getAnimations:options=>['running-descendant','pending-descendant','perpetual-decoration'].includes(fault)&&options?.subtree?[{pending:fault==='pending-descendant',playState:fault==='pending-descendant'?'idle':'running',effect:{getComputedTiming:()=>({endTime:fault==='perpetual-decoration'?Infinity:350})}}]:[]};
  const route=quick?'/ui/console/':selected?'/ui/console/projects/app-create?framework='+(fault==='wrong-id'?'react':'angular'):'/ui/console/projects/app-create';
  globalThis.location=new URL('http://fixture.localhost:8080'+route);
  globalThis.innerHeight=1000;globalThis.innerWidth=1200;
  const fonts=[{family:'Line Awesome Free',status:fault==='unloaded-font'?'error':'loaded'}];fonts.status='loaded';
  const choices=[['angular','Angular'],['astro','Astro'],['django','Django'],['dotnet','.NET'],['expressjs','Express.js'],['fastapi','FastAPI'],['fastify','Fastify'],['flask','Flask'],['flutter','Flutter'],['go','Go'],['hono','Hono'],['laravel','Laravel'],['nestjs','NestJS'],['next','Next.js'],['nuxt','Nuxt.js'],['react','React'],['qwik','Qwik'],['solidstart','SolidStart']];
  const links=choices.map(([originalId,title],i)=>{
   const id=fault==='wrong-id'&&i===1?'fabricated':originalId;
   return {...shown,href:location.origin+'/ui/console/projects/app-create?framework='+id,origin:location.origin,pathname:'/ui/console/projects/app-create',search:'?framework='+id,tabIndex:0,innerText:fault==='missing-label'?'':title,getAttribute:()=>title};
  });
  const icons=Array.from({length:quick?18:1},(_,i)=>({...shown,tabIndex:fault==='focusable-decoration'?0:-1,classList:{contains:()=>true},getAttribute:()=> 'true',closest:()=>links[i]}));
  const input={...shown,value:fault==='wrong-id'?'wrong':'Angular',tabIndex:0,getAttribute:attr=>attr==='aria-controls'?'framework-choices':'true'};
  const options=[{...shown,innerText:fault==='missing-label'?'':'Angular',getAttribute:()=> 'option'},{...shown,innerText:'Other (OIDC, SAML, API)'}];
  const panel={...shown,getAttribute:()=> 'listbox',getAnimations:()=>[],querySelectorAll:s=>s==='mat-option'?options:icons};
  globalThis.document={readyState:'complete',fonts,activeElement:input,getElementById:id=>id==='framework-choices'?panel:null,querySelectorAll:s=>!quick&&!selected&&s.startsWith('.cdk-overlay-container')?[]:icons,
   querySelector:s=>s==='cnsl-root.light-theme'?(fault==='missing-theme'?null:{}):s==='cnsl-root .main-container'?background:s==='cnsl-framework-change .framework-card'?{innerText:fault==='missing-label'?'':'Angular'}:s==='cnsl-framework-change button'?(fault==='missing-control'?null:{}):s.includes('input[role="combobox"]')?(fault==='missing-control'?null:input):null};
  const originalQuery=document.querySelectorAll;
  document.querySelectorAll=s=>!quick&&!selected&&s.startsWith('.cdk-overlay-container')?[]:s.endsWith('mat-option')?options:originalQuery(s);
  globalThis.getComputedStyle=(e,pseudo)=>e===background?{backgroundColor:'rgb(250, 250, 250)'}:pseudo?{content:'"'+String.fromCodePoint(fault==='wrong-glyph'?0xf0e0:0xf121)+'"',fontFamily:'Line Awesome Free'}:{fontSize:(quick?20:selected?40:30)+'px'};
  const resource=fault==='external-resource'?'https://outside.invalid/font.woff2':fault==='retired-resource'?'http://fixture.localhost:8080/ui/console/assets/docs/img/tech/angular.svg':'http://fixture.localhost:8080/ui/console/assets/fonts/la-solid-900.woff2';
  globalThis.performance={getEntriesByType:()=>[{name:resource}]};
  const result=frameworkObserve(kind,'light');
  assert.equal(result.glyphs&&result.labels&&result.targets&&result.themeReady&&result.resources&&result.keyboard,fault==='none'||fault==='perpetual-decoration'||quick&&fault==='missing-control',kind+'/'+fault+' '+JSON.stringify(result));
 }
}
// An asynchronous autocomplete may have an input before its panel/options exist.
// The observer must report incomplete facts so the worker polls, not throw.
const completeFrameworkDocument=document;
const completeFrameworkPanel=document.getElementById('framework-choices');
for(const pending of ['no-options','one-option','no-input']){
 const originalQuery=completeFrameworkDocument.querySelectorAll;
 globalThis.document={...completeFrameworkDocument,
  getElementById:id=>id==='framework-choices'?{...completeFrameworkPanel,querySelectorAll:s=>s==='mat-option'?(pending==='one-option'?completeFrameworkPanel.querySelectorAll(s).slice(0,1):[]):pending==='no-options'?[]:completeFrameworkPanel.querySelectorAll(s)}:null,
  querySelector:s=>pending==='no-input'&&s.includes('input[role="combobox"]')?null:completeFrameworkDocument.querySelector(s),
  querySelectorAll:s=>s.endsWith('mat-option')?(pending==='one-option'?originalQuery(s).slice(0,1):[]):s.includes('.neutral-framework-icon')&&pending==='no-options'?[]:originalQuery(s)};
 const result=frameworkObserve('framework-picker','light');
 assert.equal(result.glyphs&&result.labels&&result.targets&&result.themeReady&&result.resources&&result.keyboard,false,pending+' admitted an incomplete picker');
 assert.equal(result.keyboard,false,pending+' claimed keyboard readiness');
}
for(const association of ['global-overlay','missing-panel','wrong-role','running-panel']){
 globalThis.document={...completeFrameworkDocument,
  getElementById:id=>association==='missing-panel'?null:{...completeFrameworkPanel,getAttribute:()=>association==='wrong-role'?'dialog':'listbox',getAnimations:()=>association==='running-panel'?[{pending:false,playState:'running',effect:{getComputedTiming:()=>({endTime:120})}}]:[]},
  querySelectorAll:s=>s.endsWith('mat-option')?completeFrameworkPanel.querySelectorAll('mat-option'):completeFrameworkPanel.querySelectorAll('.neutral-framework-icon')};
 const result=frameworkObserve('framework-picker','light');
 assert.equal(result.glyphs&&result.labels&&result.targets&&result.themeReady&&result.resources&&result.keyboard,association==='global-overlay',association+' association admitted incorrectly '+JSON.stringify(result));
}
console.log('PASS: actual framework observation script - 46 native-shaped, association and asynchronous controls (unit inputs only)');
