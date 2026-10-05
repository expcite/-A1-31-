"""Build the formal design specification from the reviewed Markdown."""
from pathlib import Path
from docx import Document
from docx.shared import Cm,Pt,RGBColor
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
ROOT=Path(__file__).resolve().parents[1]
def main():
 # An unrendered Office draft stays outside the review package.
 out=ROOT.parent/'EdgeHybrid_M6_Unverified_Drafts';out.mkdir(parents=True,exist_ok=True)
 doc=Document();sec=doc.sections[0];sec.page_width=Cm(21);sec.page_height=Cm(29.7)
 sec.top_margin=Cm(2);sec.bottom_margin=Cm(2);sec.left_margin=Cm(2.2);sec.right_margin=Cm(2.2)
 for name in ['Normal','Title','Heading 1','Heading 2']:
  style=doc.styles[name];style.font.name='Microsoft YaHei';style.font.color.rgb=RGBColor(0,0,0)
  style.element.rPr.rFonts.set(qn('w:eastAsia'),'Microsoft YaHei')
 doc.styles['Normal'].font.size=Pt(10.5);doc.styles['Normal'].paragraph_format.space_after=Pt(6)
 doc.styles['Normal'].paragraph_format.line_spacing=1.25
 doc.styles['Title'].font.size=Pt(24);doc.styles['Heading 1'].font.size=Pt(16)
 lines=(ROOT/'docs/FINAL_DESIGN_REPORT.md').read_text(encoding='utf-8').splitlines();i=0
 while i<len(lines):
  line=lines[i]
  if line.startswith('|'):
   matrix=[]
   while i<len(lines) and lines[i].startswith('|'):
    if not set(lines[i].replace('|','').replace(':','').replace(' ',''))<=set('-'):
     matrix.append([x.strip() for x in lines[i].strip('|').split('|')])
    i+=1
   table=doc.add_table(rows=1,cols=len(matrix[0]));table.style='Table Grid'
   for j,row in enumerate(matrix):
    cells=table.rows[0].cells if j==0 else table.add_row().cells
    for cell,value in zip(cells,row):
     cell.text=value
     for run in cell.paragraphs[0].runs:run.font.size=Pt(9);run.bold=j==0
    # Keep small tables intact and header repeated.
    if j==0:
     header=OxmlElement('w:tblHeader');table.rows[0]._tr.get_or_add_trPr().append(header)
   doc.add_paragraph();continue
  if line.startswith('# '):doc.add_paragraph(line[2:],'Title')
  elif line.startswith('## '):doc.add_heading(line[3:],level=1)
  elif line.strip():doc.add_paragraph(line.replace('`',''))
  i+=1
 footer=sec.footer.paragraphs[0];footer.alignment=2
 footer.add_run('EdgeHybrid M6  页 ')
 field=OxmlElement('w:fldSimple');field.set(qn('w:instr'),'PAGE');footer._p.append(field)
 doc.core_properties.title='EdgeHybrid 最终设计说明书';doc.core_properties.subject='M5冻结证据和M6测试改进的最终设计说明'
 doc.save(out/'EdgeHybrid_Final_Design.docx');print('Formal specification DOCX generated.')
if __name__=='__main__':main()
