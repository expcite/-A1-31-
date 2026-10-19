"""Generate and render the final PDF specification from the Markdown source."""
from pathlib import Path
from html import escape
import subprocess
from reportlab.platypus import SimpleDocTemplate,Paragraph,Spacer,Table,TableStyle
from reportlab.lib.styles import ParagraphStyle
from reportlab.lib import colors
from reportlab.lib.pagesizes import A4
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
ROOT=Path(__file__).resolve().parents[1]
def main():
 pdfmetrics.registerFont(TTFont('Song','C:/Windows/Fonts/simsun.ttc',subfontIndex=0))
 pdfmetrics.registerFont(TTFont('Hei','C:/Windows/Fonts/simhei.ttf'))
 out=ROOT/'deliverables/M6';out.mkdir(parents=True,exist_ok=True)
 pdf=out/'EdgeHybrid_Final_Design.pdf'
 styles=dict(body=ParagraphStyle('body',fontName='Song',fontSize=11,leading=17,spaceAfter=9,wordWrap='CJK'),title=ParagraphStyle('title',fontName='Hei',fontSize=22,leading=30,spaceAfter=16),heading=ParagraphStyle('heading',fontName='Hei',fontSize=15,leading=23,spaceBefore=12,spaceAfter=8,keepWithNext=True),cell=ParagraphStyle('cell',fontName='Song',fontSize=9,leading=13,wordWrap='CJK'))
 lines=(ROOT/'docs/FINAL_DESIGN_REPORT.md').read_text(encoding='utf-8').splitlines();flow=[];i=0
 while i<len(lines):
  s=lines[i]
  if s.startswith('|'):
   matrix=[]
   while i<len(lines) and lines[i].startswith('|'):
    if not set(lines[i].replace('|','').replace(':','').replace(' ',''))<=set('-'):
     matrix.append([Paragraph(escape(x.strip()),styles['cell']) for x in lines[i].strip('|').split('|')])
    i+=1
   widths=[72,423] if len(matrix[0])==2 else [495/len(matrix[0])]*len(matrix[0])
   table=Table(matrix,colWidths=widths,repeatRows=1,hAlign='LEFT');table.setStyle(TableStyle([('BACKGROUND',(0,0),(-1,0),colors.HexColor('#E7EDF1')),('GRID',(0,0),(-1,-1),.4,colors.HexColor('#87929A')),('VALIGN',(0,0),(-1,-1),'TOP'),('LEFTPADDING',(0,0),(-1,-1),6),('RIGHTPADDING',(0,0),(-1,-1),6),('TOPPADDING',(0,0),(-1,-1),6),('BOTTOMPADDING',(0,0),(-1,-1),6)]));flow.extend([table,Spacer(1,10)]);continue
  if s.strip():
   key='title' if s.startswith('# ') else 'heading' if s.startswith('## ') else 'body'
   s=s[2:] if key=='title' else s[3:] if key=='heading' else s
   flow.append(Paragraph(escape(s.replace('`','')),styles[key]))
  i+=1
 def footer(canvas,doc):
  canvas.setFont('Song',9);canvas.drawString(50,27,'EdgeHybrid M6 最终设计说明书');canvas.drawRightString(545,27,f'{doc.page}')
 doc=SimpleDocTemplate(str(pdf),pagesize=A4,rightMargin=50,leftMargin=50,topMargin=44,bottomMargin=44,title='EdgeHybrid 最终设计说明书',author='EdgeHybrid 项目',pageCompression=1)
 doc.build(flow,onFirstPage=footer,onLaterPages=footer)
 render=ROOT/'results/raw/m6_20261001/pdf_render';render.mkdir(exist_ok=True)
 poppler='C:/Users/[REDACTED_USER]/.cache/local_review-runtimes/local_review-primary-runtime/dependencies/native/poppler/Library/bin/pdftoppm.exe'
 subprocess.run([poppler,'-r','130','-png',str(pdf),str(render/'page')],check=True)
 print('PDF generated and rendered:',pdf)
if __name__=='__main__':main()
