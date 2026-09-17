"""Production finish of imagegen layouts using the authoritative traced mark.
No generated logo silhouette is used in final outputs. Typography is outlined.
"""
from pathlib import Path
import io,re,subprocess
import cairo
root=Path(__file__).resolve().parents[1]
INK='#2E2A26';PAPER='#F4F1E9';SAND='#D6CDBF';TAUPE='#A89B8E';STONE='#E8E2D9'
d=re.search(r' d="([^"]+)"',(root/'logo/mark.svg').read_text()).group(1)
def rect(x,y,w,h,fill):return f'<rect x="{x}" y="{y}" width="{w}" height="{h}" fill="{fill}"/>'
def line(x1,y1,x2,y2,color=SAND,dash=''):
 return f'<path d="M{x1} {y1}H{x2}" fill="none" stroke="{color}"/>' if y1==y2 and not dash else f'<path d="M{x1} {y1}L{x2} {y2}" fill="none" stroke="{color}"'+(f' stroke-dasharray="{dash}"' if dash else '')+'/>'
def mark(x,y,size,inverse=False):return f'<g transform="translate({x} {y}) scale({size/800})"><path fill="{PAPER if inverse else INK}" d="{d}"/></g>'
def text(s,size,x,y,tracking=0,align='left',color=INK):
 b=io.BytesIO();surface=cairo.SVGSurface(b,1800,1200);c=cairo.Context(surface)
 c.select_font_face('Liberation Sans',cairo.FONT_SLANT_NORMAL,cairo.FONT_WEIGHT_NORMAL);c.set_font_size(size)
 advances=[c.text_extents(ch).x_advance for ch in s];width=sum(advances)+tracking*(len(s)-1)
 if align=='center':x-=width/2
 if align=='right':x-=width
 for ch,w in zip(s,advances):c.move_to(x,y);c.text_path(ch);x+=w+tracking
 rgb=[int(color[i:i+2],16)/255 for i in [1,3,5]];c.set_source_rgb(*rgb);c.fill();surface.finish()
 return ''.join(re.findall(r'<path[^>]+/>',b.getvalue().decode()))
def save(name,w,h,body,title):
 s=f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}"><title>{title}</title>'+rect(0,0,w,h,PAPER)+body+'</svg>\n'
 p=root/f'boards/{name}.svg';p.write_text(s)
 subprocess.run(['rsvg-convert',str(p),'-o',str(p.with_suffix('.png'))],check=True)
# The imagegen horizontal composition, finished with the unchanged vector mark.
b=mark(300,117,400)+text('TRELLIS',139,760,365,25)
save('readme-header',1800,600,b,'Trellis — README and GitHub header')
# The imagegen social composition at the required exact output dimensions.
b=mark(239,160,220)+text('TRELLIS',91,482,303,16)
b+=text('Knowledge in context moves forward.',31,600,411,1.3,'center')
save('og-social',1200,630,b,'Trellis — Knowledge in context moves forward.')
# Same six-panel layout and typographic hierarchy as the generated sheet.
b=text('TRELLIS',61,44,95,12)+text('LOGO SYSTEM',22,47,128,5)
b+=line(1120,50,1120,125)+text('KNOWLEDGE',14,1162,65,5)+text('IN CONTEXT',14,1162,88,5)+text('MOVES FORWARD.',14,1162,111,5)
b+=line(35,161,1417,161)+line(35,609,1417,609)+line(494,161,494,970)+line(954,161,954,970)
for s,x,y in [('01   MARK',45,196),('02   CLEARSPACE',519,196),('03   CONSTRUCTION',983,196),('04   SIZE RAMP',45,645),('05   MONO',519,645),('06   INVERSE',983,645)]:b+=text(s,13,x,y,2.7)
b+=mark(82,230,375)
# x=80 source units of optical clearspace around silhouette bounds.
# Reference-coordinate silhouette bounds approximately (61,99)-(691,687).
b+=mark(572,245,340)
left=572+61*.425;right=572+691*.425;top=245+99*.425;bottom=245+687*.425;xspace=34
for x in [left-xspace,left,right,right+xspace]:b+=line(x,top-xspace,x,bottom+xspace,SAND,'5 5')
for y in [top-xspace,top,bottom,bottom+xspace]:b+=line(left-xspace,y,right+xspace,y,SAND,'5 5')
b+=text('x',14,left-xspace/2,top-12,0,'center',TAUPE)+text('x',14,right+xspace/2,bottom+22,0,'center',TAUPE)
# Observational alignment grid, not an assertion of original geometric derivation.
for x in range(1013,1371,21):b+=line(x,224,x,578)
for y in range(224,579,21):b+=line(1013,y,1370,y)
b+=mark(1025,242,350)
b+=text('ALIGNMENT GRID',11,1192,597,2,'center',TAUPE)
# Real raster-canvas sizes; no enlarged stand-ins labelled as pixels.
for cx,n in [(135,128),(301,32),(427,16)]:
 b+=mark(cx-n/2,839-n/2,n)
 b+=text(str(n)+' px',14,cx,915,0,'center')
for cx,label in [(135,'Primary use'),(301,'Interface'),(427,'Favicon')]:b+=text(label,13,cx,938,0,'center',TAUPE)
b+=mark(595,660,320)+rect(975,659,436,307,INK)+mark(1035,660,320,True)
b+=text('TRELLIS',12,35,1034,3)+line(137,1021,1070,1021)+text('A MORE CONNECTED MIND',12,1410,1034,3,'right')
save('logo-construction',1448,1086,b,'Trellis — Logo construction, clearspace and size system')
