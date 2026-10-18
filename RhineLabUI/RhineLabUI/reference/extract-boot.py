import sys
from pathlib import Path
root=next(p for p in Path(__file__).resolve().parents if any(p.glob('*.mp4')))
for parent in Path(__file__).resolve().parents:
 if (parent/'.tools').is_dir():sys.path.insert(0,str(parent/'.tools'))
import cv2
from PIL import Image,ImageDraw
out=Path('reference/boot-frames');out.mkdir(parents=True,exist_ok=True)
v=cv2.VideoCapture(str(next(root.glob('*.mp4'))))
print('VIDEO',v.get(3),v.get(4),v.get(5),v.get(7))
v.set(cv2.CAP_PROP_POS_FRAMES,125)
for fr in range(125,676):
 ok,im=v.read()
 if not ok:break
 cv2.imwrite(str(out/f'{fr:04}.jpg'),im,[cv2.IMWRITE_JPEG_QUALITY,94])
sheet=Image.new('RGB',(1600,1845),'#242424');d=ImageDraw.Draw(sheet)
for i,fr in enumerate(range(125,676,12)):
 im=Image.open(out/f'{fr:04}.jpg');im.thumbnail((320,180));x=i%5*320;y=i//5*200
 sheet.paste(im,(x,y+20));d.text((x+8,y+3),f'{fr/25:.2f}s / {fr}',fill='white')
sheet.save(out/'overview.jpg',quality=95)
for sec in range(5,27):
 sheet=Image.new('RGB',(1920,1200),'#242424');d=ImageDraw.Draw(sheet)
 for i in range(25):
  fr=sec*25+i;im=Image.open(out/f'{fr:04}.jpg');im.thumbnail((384,216));x=i%5*384;y=i//5*240
  sheet.paste(im,(x,y+24));d.text((x+8,y+5),f'{fr/25:.2f}s / {fr}',fill='white')
 sheet.save(out/f'second-{sec:02}.jpg',quality=95)
print('Extracted',551,'frames and 22 consecutive-frame sheets')
