// Unit controls for the exact scripts executed by the native browser worker.
// Synthetic inputs here are not native rendering, federation or activation proof.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const source=readFileSync(new URL('./zitadel-browser.go',import.meta.url),'utf8');
const script=name=>{const match=source.match(new RegExp('const '+name+' = `([\\s\\S]*?)`'));assert.ok(match,name);return new Function(match[1]);};
const observe=script('browserProviderObservation');
globalThis.location=new URL('http://fixture.localhost:8080/ui/login/login');
globalThis.document={querySelectorAll:()=>[],querySelector:()=>null,documentElement:{classList:{contains:()=>true}},readyState:'complete',fonts:{status:'loaded'}};
for(const resource of ['http://fixture.localhost:8080/ui/login/resources/images/idp/google.png','http://fixture.localhost:8080/ui/console/assets/images/idp/apple.svg','https://outside.invalid/font.woff2']){
 globalThis.performance={getEntriesByType:()=>[{name:resource}]};
 assert.equal(observe('login','light').resources,false,'retired/external native resource admitted');
}
globalThis.performance={getEntriesByType:()=>[{name:'http://fixture.localhost:8080/ui/login/resources/fonts/lgn-icons.woff'}]};
assert.equal(observe('login','light').resources,true,'ordinary local resource refused');
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
  return {ok:mode!=='http-error',headers:{get:()=> 'application/json'},text:async()=>mode==='malformed-body'?'incomplete JSON':JSON.stringify(response)};
 };
 const result=await new Promise(resolve=>setup(resolve));
 assert.equal(result.complete,mode==='positive',`${mode} returned wrong verdict`);
 if(mode==='positive'){assert.equal(result.count,2);assert.equal(result.phase,4);assert.deepEqual(result.ids,ids);assert.equal(requests.length,11);}
 else{assert.equal(result.count,0);assert.equal(JSON.stringify(result).includes('fictional-unit-bearer'),false);}
}
console.log('PASS: actual observation-script resource controls and ten closed fixture API cases (unit inputs only)');
