# Adapted replay operation; not a historical receipt. See provenance.json.
from pathlib import Path
import subprocess,json,datetime,hashlib,os
p=Path('__RUN_ROOT__'); image='sha256:ba88399f2fd98018292af5c4f65fe924a6cc46698c84570d59294693ce58c562'
results=[]
for name,module,args in [('build.test','build',['test','-c','-tags=livebuild','-o','/out/bin/build.test','./internal/build']),('fabric','fabric',['build','-o','/out/fabric','./cmd/fabric']),('serve','serve',['build','-o','/out/serve','./cmd/server']),('first-deploy.test','workspace',['test','-c','-tags=livebuild','-o','/out/first-deploy.test','./internal/launch'])]:
 cmd=['docker','run','--rm','--cpus','4','--label','opl.probe='+p.name,'-e','CGO_ENABLED=0','-e','GOPROXY=https://goproxy.cn','-e','GOCACHE=/out/go-build-cache','-e','GOMODCACHE=/out/go-mod-cache','-e','GOMAXPROCS=4','-v',str(p/'source')+':/src:ro','-v',str(p)+':/out','--entrypoint','go',image,'-C','/src/services/'+module,*args]
 print('COMPILING',name,flush=True)
 with (p/'evidence'/('compile-retry-'+name+'.log')).open('w') as log:r=subprocess.run(cmd,stdout=log,stderr=subprocess.STDOUT)
 item={'binary':name,'command':cmd,'exitCode':r.returncode}
 if r.returncode==0:item['sha256']='sha256:'+hashlib.sha256((p/('bin/'+name if name=='build.test' else name)).read_bytes()).hexdigest()
 results.append(item);(p/'evidence/compilation-retry.receipt.json').write_text(json.dumps(results,indent=2)+'\n')
 if r.returncode:print((p/'evidence'/('compile-retry-'+name+'.log')).read_text()[-4000:],flush=True);raise SystemExit(r.returncode)
 print('COMPILED',name,flush=True)
