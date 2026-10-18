import sys
sys.path.insert(0,'.tools')
import cv2
from PIL import Image,ImageDraw
from pathlib import Path
v=cv2.VideoCapture(str(next(Path('.').glob('*.mp4'))))
print('VIDEO',v.get(3),v.get(4),v.get(5),v.get(7))
sheet=Image.new('RGB',(1280,1800),'#202020'); d=ImageDraw.Draw(sheet)
for i,t in enumerate(range(5,41,2)):
 v.set(0 if False else cv2.CAP_PROP_POS_MSEC,t*1000); ok,f=v.read()
 if ok:
  cv2.imwrite(f'reference/{t:02}.jpg',f)
  im=Image.fromarray(cv2.cvtColor(f,cv2.COLOR_BGR2RGB)); im.thumbnail((640,340)); x=(i%2)*640; y=(i//2)*200
  im.thumbnail((640,180)); sheet.paste(im,(x,y+20)); d.text((x+5,y+3),str(t)+'s',fill='white')
sheet.save('reference/contact.jpg')
