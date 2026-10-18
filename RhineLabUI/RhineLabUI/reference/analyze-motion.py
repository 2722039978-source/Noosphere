import sys
sys.path.insert(0,'.tools')
import cv2
from PIL import Image, ImageDraw
from pathlib import Path
out=Path('reference/motion');out.mkdir(parents=True,exist_ok=True)
v=cv2.VideoCapture(str(next(Path('.').glob('*.mp4'))));fps=v.get(cv2.CAP_PROP_FPS)
v.set(cv2.CAP_PROP_POS_FRAMES,26*25)
for frame in range(650,1001):
 ok,im=v.read()
 if not ok:break
 cv2.imwrite(str(out/f'{frame:04}.jpg'),im,[cv2.IMWRITE_JPEG_QUALITY,88])
for start in [27,29,31,33,35,37]:
 sheet=Image.new('RGB',(1920,1536),'#282828');d=ImageDraw.Draw(sheet)
 for i in range(12):
  fr=round((start+i/6)*25);im=Image.open(out/f'{fr:04}.jpg');im.thumbnail((640,360));x=(i%3)*640;y=(i//3)*384
  sheet.paste(im,(x,y+24));d.text((x+12,y+5),f'{fr/25:.2f}s  FRAME {fr}',fill='white')
 sheet.save(out/f'sequence-{start}.jpg',quality=94)
print('Extracted 351 consecutive frames at 25fps and 6 contact sheets')
