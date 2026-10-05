"""Render the editable Chinese manual with embedded local fonts and screenshots."""
from pathlib import Path
import argparse,html
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.lib.styles import ParagraphStyle
from reportlab.lib import colors
from reportlab.platypus import SimpleDocTemplate,Paragraph,Spacer,Image,PageBreak
from reportlab.lib.enums import TA_LEFT
from reportlab.lib.pagesizes import A4
root=Path(__file__).resolve().parents[1]
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--source',required=True);ap.add_argument('--out',required=True);a=ap.parse_args()
 pdfmetrics.registerFont(TTFont('Chinese',r'C:\Windows\Fonts\simhei.ttf'))
 styles={k:ParagraphStyle(k,fontName='Chinese',fontSize=size,leading=lead,textColor=colors.HexColor('#19384d'),spaceAfter=space,wordWrap='CJK') for k,size,lead,space in [('body',10,16,7),('h1',23,31,16),('h2',16,24,12),('h3',12,19,10)]}
 story=[]
 for line in Path(a.source).read_text(encoding='utf-8').splitlines():
  if not line.strip():continue
  if line.startswith('![截图]('):
   p=root/line[len('![截图]('):-1];img=Image(str(p));w=480;h=img.imageHeight*w/img.imageWidth
   if h>480:w*=480/h;h=480
   img.drawWidth=w;img.drawHeight=h;story.extend([img,Spacer(1,12)]);continue
  if line=='---':story.append(PageBreak());continue
  kind='body';text=line
  for prefix,key in [('### ','h3'),('## ','h2'),('# ','h1')]:
   if line.startswith(prefix):kind=key;text=line[len(prefix):];break
  story.append(Paragraph(html.escape(text),styles[kind]))
 out=Path(a.out);out.parent.mkdir(parents=True,exist_ok=True)
 def footer(canvas,doc):canvas.setFont('Chinese',8);canvas.setFillColor(colors.HexColor('#688091'));canvas.drawString(44,24,'EdgeHybrid 交互通信平台 | 本机模拟 / 真实安全TCP');canvas.drawRightString(A4[0]-44,24,str(doc.page))
 SimpleDocTemplate(str(out),pagesize=A4,rightMargin=44,leftMargin=44,topMargin=42,bottomMargin=42,title='EdgeHybrid交互平台用户与复现手册',author='EdgeHybrid').build(story,onFirstPage=footer,onLaterPages=footer)
 print(str(out))
if __name__=='__main__':main()
