import json,sys
from datetime import datetime,timedelta
W={'adiabatic-fr','analog-imc-fr','cpu-fr','cpu-gb','ising-digital-pl','neuromorphic-pl','optical-ising-gb'}
def t(s): return datetime.fromisoformat(s.replace('Z','+00:00')[:19]+'+00:00') if s else None
cut0=t('2026-11-08T17:32:35Z'); cut1=t('2026-11-08T17:34:35Z')
d=json.load(open(sys.argv[1]))
rows=[]
for it in d['items']:
    st=it['status']; m=it['metadata']
    rep=max(t(f['time']) for f in m.get('managedFields',[]) if f.get('subresource')=='status')
    rows.append(dict(name=m['name'],planned=t(st.get('plannedAt')),plan=st['plan']['substrate'],by=st.get('claimedBy'),claimed=t(st.get('claimedAt')),rep=rep,phase=st['phase'],reasons=st.get('reasons',[])))
onw=[r for r in rows if r['by'] in W]
print('total',len(rows),'phases',{p:sum(r['phase']==p for r in rows) for p in set(r['phase'] for r in rows)})
print('claimed by worker substrates:',len(onw),' by control-plane substrates:',len(rows)-len(onw))
during=[r for r in rows if r['claimed'] and cut0<=r['claimed']<cut1]
print('claimed during cut:',len(during),'of them by worker:',sum(r['by'] in W for r in during))
moved=[r for r in rows if r['plan'] in W and r['by'] not in W]
print('planned for worker, finished on control plane:',len(moved))
late=[r for r in onw if r['claimed']<cut1 and r['rep']>=cut1]
for r in sorted(onw,key=lambda r:r['claimed']):
    if r['claimed']>=cut0-timedelta(seconds=30) and r['claimed']<=cut1+timedelta(seconds=10):
        print(' ',r['name'],r['by'],'claimed',r['claimed'].time(),'reported',r['rep'].time(),r['phase'])
print('worker claims before/at cut reported after link returned:',len([r for r in onw if r['claimed']<cut0+timedelta(seconds=5) and r['rep']>=cut1]))
gap=[r for r in onw if cut0<=r['claimed']<cut1]
print('worker claims inside the cut window:',len(gap))
