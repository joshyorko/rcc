#!/usr/bin/env python3
"""Read-only physical-allocation metadata census for the frozen RCC fixture."""
import hashlib, json, os, pathlib, resource, stat, sys, time

OUT=pathlib.Path(__file__).resolve().parent
PROVIDER=pathlib.Path('/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/attempt-10/attempt-10/provider-content')
OBJECTS=PROVIDER/'objects'
INDEX=PROVIDER/'objects/sha256/e5/87/e587078b0b8d8bce38d4e665999b9a3a0d4d9fb1de1b87a8c3a4b223e71e50f6'
PACKDIR=pathlib.Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/pack-layout-micro')
OFFSET=PACKDIR/'offset-index.json'
PACK=PACKDIR/'small-objects.concat'
EXPECTED={'index':('e587078b0b8d8bce38d4e665999b9a3a0d4d9fb1de1b87a8c3a4b223e71e50f6',1491800),'offset':('e8dca8fa7d069883441bd8616e49234d338ab5707b3cc05d6b3a5693b2ba99e2',525841),'pack':('44d2341ac7cecf8a407e881bd4514e1f4fb6dd59530a4d172049d5c7e40f39a9',4484936)}
CAP=2*1024*1024

def assert_no_symlink_components(path):
    path=pathlib.Path(path).absolute()
    cur=pathlib.Path(path.anchor)
    for part in path.parts[1:]:
        cur=cur/part
        st=cur.lstat()
        if stat.S_ISLNK(st.st_mode): raise RuntimeError(f'symlink path component: {cur}')
        if cur!=path and not stat.S_ISDIR(st.st_mode): raise RuntimeError(f'non-directory path component: {cur}')
    if not stat.S_ISREG(path.lstat().st_mode): raise RuntimeError(f'not a regular file: {path}')

def hash_file(path):
    assert_no_symlink_components(path)
    h=hashlib.sha256(); n=0
    with path.open('rb') as f:
        while True:
            b=f.read(1024*1024)
            if not b: break
            h.update(b); n+=len(b)
    return n,h.hexdigest()

def allocation(st): return int(st.st_blocks)*512

def must_regular(path):
    # Verify each component under objects root without following a symlink.
    rel=path.relative_to(OBJECTS)
    cur=OBJECTS
    rootst=cur.lstat()
    if stat.S_ISLNK(rootst.st_mode) or not stat.S_ISDIR(rootst.st_mode): raise RuntimeError('unsafe objects root')
    parts=rel.parts
    for part in parts[:-1]:
        cur=cur/part; ds=cur.lstat()
        if stat.S_ISLNK(ds.st_mode) or not stat.S_ISDIR(ds.st_mode): raise RuntimeError(f'unsafe directory component: {cur}')
    st=path.lstat()
    if stat.S_ISLNK(st.st_mode) or not stat.S_ISREG(st.st_mode): raise RuntimeError(f'not a regular non-symlink file: {path}')
    return st

def mount_record(path):
    resolved=path.resolve(strict=True)
    best=None
    raw=pathlib.Path('/proc/self/mountinfo').read_text().splitlines()
    for line in raw:
        left,right=line.split(' - ',1); f=left.split(); tail=right.split()
        mp=f[4].replace('\\040',' ').replace('\\011','\t').replace('\\134','\\')
        mpp=pathlib.Path(mp)
        if resolved==mpp or mpp in resolved.parents:
            if best is None or len(mp)>len(best['mount_point']):
                v=os.statvfs(path)
                best={'mount_id':int(f[0]),'mount_point':mp,'mount_options':f[5],'filesystem_type':tail[0],'source':tail[1],'super_options':tail[2] if len(tail)>2 else '', 'device':f[2], 'statvfs':{'block_size':v.f_frsize,'blocks':v.f_blocks,'free_blocks':v.f_bfree,'available_blocks':v.f_bavail,'total_bytes':v.f_blocks*v.f_frsize,'available_bytes':v.f_bavail*v.f_frsize}}
    if best is None: raise RuntimeError(f'no mountinfo entry for {path}')
    return best

def main():
    started=time.monotonic()
    # Hash the three frozen metadata/pack inputs. Object payloads are never opened.
    input_receipts={}
    for name,p in [('index',INDEX),('offset',OFFSET),('pack',PACK)]:
        n,h=hash_file(p); eh,es=EXPECTED[name]
        if (n,h)!=(es,eh): raise RuntimeError(f'{name} size/hash mismatch: {(n,h)}')
        input_receipts[name]={'path':str(p),'size_bytes':n,'sha256':h}
    idx=json.loads(INDEX.read_bytes()); off=json.loads(OFFSET.read_bytes())
    entries=idx['entries']; offsets=off['entries']
    if idx.get('count')!=5537 or len(entries)!=5537: raise RuntimeError('index count is not frozen 5537')
    if sum(x['storedSize'] for x in entries)!=idx['totalStoredBytes'] or sum(x['logicalSize'] for x in entries)!=idx['totalLogicalBytes']: raise RuntimeError('index totals mismatch')
    small=sorted((x for x in entries if x['storedSize']<4096),key=lambda x:x['storedDigest'])
    if len(small)!=4649 or len(offsets)!=4649: raise RuntimeError('selected object count mismatch')
    if [e['digest'] for e in offsets]!=[x['storedDigest'] for x in small]: raise RuntimeError('offset index selection/order mismatch')
    if off.get('pack_bytes')!=EXPECTED['pack'][1]: raise RuntimeError('pack length declaration mismatch')
    offset=0
    for x,e in zip(small,offsets):
        if e.get('offset')!=offset or e.get('size')!=x['storedSize']: raise RuntimeError('offset coverage mismatch')
        offset+=e['size']
    if offset!=EXPECTED['pack'][1]: raise RuntimeError('offset index does not cover pack')
    by_digest={x['storedDigest']:x for x in entries}
    small_set={x['storedDigest'] for x in small}
    records=[]; dirs=set(); small_dirs=set(); inode_map={}; small_inode_map={}
    for ent in entries:
        digest=ent['storedDigest'].removeprefix('sha256:')
        p=OBJECTS/'sha256'/digest[:2]/digest[2:4]/digest
        st=must_regular(p)
        if st.st_size!=ent['storedSize']: raise RuntimeError(f'stored size mismatch: {digest}')
        rel=p.relative_to(OBJECTS).as_posix(); cur=p.parent
        while cur!=OBJECTS:
            ds=cur.lstat()
            if stat.S_ISLNK(ds.st_mode) or not stat.S_ISDIR(ds.st_mode): raise RuntimeError(f'unsafe directory: {cur}')
            dirs.add(cur)
            if ent['storedDigest'] in small_set: small_dirs.add(cur)
            cur=cur.parent
        inode=(st.st_dev,st.st_ino)
        inode_map.setdefault(inode,{'dev':st.st_dev,'ino':st.st_ino,'allocated_bytes':allocation(st),'size_bytes':st.st_size,'nlink':st.st_nlink})
        if ent['storedDigest'] in small_set: small_inode_map.setdefault(inode,inode_map[inode])
        records.append({'d':ent['storedDigest'],'p':rel,'stored':ent['storedSize'],'logical':ent['logicalSize'],'alloc':allocation(st),'dev':st.st_dev,'ino':st.st_ino,'nlink':st.st_nlink,'uid':st.st_uid,'mode':stat.S_IMODE(st.st_mode)})
    def dir_summary(paths):
        rows=[]; keys=set()
        for p in sorted(paths):
            st=p.lstat(); inode=(st.st_dev,st.st_ino); keys.add(inode)
            rows.append((inode,allocation(st),st.st_size))
        return {'unique_directory_paths':len(paths),'sum_path_allocated_bytes':sum(x[1] for x in rows),'unique_directory_inodes':len(keys),'unique_inode_allocated_bytes':sum({x[0]:x[1] for x in rows}.values())}
    all_dir=dir_summary(dirs); small_dir=dir_summary(small_dirs); shared_dirs=dirs&small_dirs; full_only_dirs=dirs-small_dirs
    small_entries=[x for x in entries if x['storedDigest'] in small_set]
    pack_st=PACK.lstat(); offset_st=OFFSET.lstat()
    if not stat.S_ISREG(pack_st.st_mode) or not stat.S_ISREG(offset_st.st_mode): raise RuntimeError('retained pack/offset not regular files')
    mounts={str(p):mount_record(p) for p in (PROVIDER,INDEX,PACK,OFFSET,OUT)}
    rss=resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024
    if rss>128*1024*1024: raise RuntimeError(f'observed peak RSS exceeds 128 MiB: {rss}')
    result={'status':'PASS','numeric_exit':0,'claim_boundary':'Metadata allocation census only. st_blocks*512 is allocated-block accounting, not physical unique disk usage, reflink/overlay lower-layer duplication, or performance gain. No payload bytes were opened. No object was removed; pack-index/public-manifest verification overhead remains required.','input_hashes':input_receipts,'index_summary':{'entry_count':len(entries),'total_stored_bytes':idx['totalStoredBytes'],'total_logical_bytes':idx['totalLogicalBytes'],'small_count_lt_4096':len(small_entries),'small_stored_bytes':sum(x['storedSize'] for x in small_entries),'small_logical_bytes':sum(x['logicalSize'] for x in small_entries)},'objects_summary':{'path_count':len(records),'path_allocated_bytes':sum(x['alloc'] for x in records),'unique_inode_count':len(inode_map),'unique_inode_allocated_bytes':sum(x['allocated_bytes'] for x in inode_map.values()),'hardlink_paths_beyond_unique_inodes':len(records)-len(inode_map),'small_path_count':len(small_entries),'small_path_allocated_bytes':sum(x['alloc'] for x in records if x['d'] in small_set),'small_unique_inode_count':len(small_inode_map),'small_unique_inode_allocated_bytes':sum(x['allocated_bytes'] for x in small_inode_map.values()),'small_hardlink_paths_beyond_unique_inodes':len(small_entries)-len(small_inode_map)},'directories':{'full':all_dir,'small_subset':small_dir,'shared_subset_paths':dir_summary(shared_dirs),'full_only_paths':dir_summary(full_only_dirs),'small_is_subset_of_full':small_dirs<=dirs},'retained_pack':{'path':str(PACK),'size_bytes':pack_st.st_size,'allocated_bytes':allocation(pack_st),'dev':pack_st.st_dev,'ino':pack_st.st_ino,'nlink':pack_st.st_nlink,'uid':pack_st.st_uid,'mode':stat.S_IMODE(pack_st.st_mode)},'retained_offset_index':{'path':str(OFFSET),'size_bytes':offset_st.st_size,'allocated_bytes':allocation(offset_st),'dev':offset_st.st_dev,'ino':offset_st.st_ino,'nlink':offset_st.st_nlink,'uid':offset_st.st_uid,'mode':stat.S_IMODE(offset_st.st_mode)},'mounts':mounts,'resource':{'elapsed_seconds':round(time.monotonic()-started,4),'observed_peak_rss_bytes':rss,'rss_limit_bytes':128*1024*1024,'output_cap_bytes':CAP,'timeout_seconds':30,'payload_file_opens':0,'payload_bytes_read':0},'file_records':records}
    raw=json.dumps(result,separators=(',',':'),sort_keys=True).encode()+b'\n'
    existing_alloc=sum((p.lstat().st_blocks*512) for p in OUT.iterdir() if p.is_file())
    padded=(len(raw)+4095)//4096*4096
    if existing_alloc+padded+65536>CAP: raise RuntimeError(f'census aggregate allocation would exceed {CAP}-byte cap')
    dst=OUT/'census.json'
    fd=os.open(dst,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    try:
        with os.fdopen(fd,'wb') as f: f.write(raw);f.flush();os.fsync(f.fileno())
    except BaseException:
        try: dst.unlink()
        except OSError: pass
        raise
    print(json.dumps({'status':'PASS','numeric_exit':0,'output_bytes':len(raw),'file_records':len(records),'peak_rss_bytes':rss,'elapsed_seconds':round(time.monotonic()-started,4)}))
    return 0

if __name__=='__main__':
    try: raise SystemExit(main())
    except BaseException as e:
        print(json.dumps({'status':'FAIL','numeric_exit':1,'error':f'{type(e).__name__}: {e}'}),file=sys.stderr)
        raise
