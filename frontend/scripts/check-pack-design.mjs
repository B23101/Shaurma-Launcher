// Regression: applying a design must not subscribe its loader to its own state.
import {readFile,writeFile,mkdir} from 'node:fs/promises'
import {spawnSync} from 'node:child_process'
import {createRequire} from 'node:module'
import {fileURLToPath} from 'node:url'
const require=createRequire(import.meta.url)
const {compileModule}=require('svelte/compiler')
const source=await readFile(new URL('../src/lib/PackPageDesign.svelte',import.meta.url),'utf8')
const dir=fileURLToPath(new URL('../node_modules/.cache/mafia-regression/',import.meta.url))
await mkdir(dir,{recursive:true})
function harness(source){
 source=source.replaceAll("\r\n","\n")
 const start=source.indexOf('  $effect(() => {\n    const id = packId')
 const end=source.indexOf('\n  function toggle()',start)
 if(start<0||end<0)throw Error('Cannot locate actual design loading effect')
 const effect=source.slice(start,end).replace('(source: string)', '(source)').replace('$effect(() => {','$effect(() => {\n loadRuns++;')
 return `import {untrack,flushSync} from 'svelte';
 let loadRuns=0,requests=0,selectPack,selectOfficial;
 const mafia='fixture';
 const localStorage={getItem:()=>null};
 class DOMParser {parseFromString(){return {querySelector:s=>({getAttribute:()=>s.includes('skin')?'mafia':s.includes('background')?'':'#ffc83d'}),querySelectorAll:()=>[],createElement:()=>({}),head:{prepend(){}},documentElement:{outerHTML:'<html></html>'}}}}
 const App={GetPackPageDesign:()=>{requests++;return Promise.resolve('fixture')}};
 const cleanup=$effect.root(()=>{
 let packId=$state('mafia'),official=$state(true),html=$state(''),palette=$state(''),enabled=$state(true),skin=$state(''),scene=$state('');let designKey='';
 selectPack=id=>packId=id;selectOfficial=value=>official=value;
 ${effect}
 });
 async function settle(){flushSync();await Promise.resolve();flushSync();await Promise.resolve();flushSync()}
 await settle();
 if(loadRuns!==1||requests!==1)throw Error('Initial load repeated: '+loadRuns+'/'+requests);
 selectPack('other');await settle();
 if(loadRuns!==2||requests!==2)throw Error('Pack switch repeated: '+loadRuns+'/'+requests);
 selectOfficial(false);await settle();
 if(loadRuns!==3||requests!==2)throw Error('Custom pack triggered a request');
 cleanup();console.log('PASS: initial design, async result, pack switch and custom pack remain stable');
 `
}

const {js}=compileModule(harness(source),{filename:'pack-design-regression.svelte.js',generate:'client'})
const file=dir+'regression.mjs'
await writeFile(file,js.code,'utf8')
const result=spawnSync(process.execPath,['--conditions=browser',file],{encoding:'utf8',timeout:10000})
if(result.status!==0)throw Error(result.stderr||String(result.error))
console.log(result.stdout.trim())
