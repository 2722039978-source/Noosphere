from pathlib import Path
import sys
sys.path.insert(0,'.tools')
import cv2
import numpy as np
sources=[
 'C:/Users/LBLC/AppData/Local/Temp/codex-clipboard-159c6dd1-8880-4ed7-8638-037d13c5a65b.png',
 'C:/Users/LBLC/AppData/Local/Temp/codex-clipboard-c945190d-1847-4dcd-9595-333623f8d757.png',
 'C:/Users/LBLC/AppData/Local/Temp/codex-clipboard-c37ac867-c3a5-4f49-bf2d-d66def143131.png']
frames=[(p,cv2.resize(cv2.imread(str(p)),(480,270)).astype(float)) for p in Path('reference/motion').glob('[0-9]*.jpg')]
for source in sources:
    im=cv2.imread(source)[:1080,:1920]
    im=cv2.resize(im,(480,270)).astype(float)
    mask=~((im[:,:,2]>170)&(im[:,:,1]>130)&(im[:,:,0]<160))
    mask[:55,:90]=False
    scores=sorted((np.mean(np.abs(im-f)[mask]),p.stem) for p,f in frames)
    print(Path(source).name,[(int(n)/25,round(float(s),2)) for s,n in scores[:4]])
