from pathlib import Path
import io, re, subprocess
import cairo
root=Path(__file__).resolve().parents[1]
d='M397 99 C291 97 187 143 156 191 C137 220 151 260 175 275 C211 304 268 309 332 320 C398 331 440 344 474 380 C498 410 515 443 568 443 C625 444 670 405 685 356 C704 289 677 219 622 176 C565 125 480 100 397 99 Z M194 360 C123 358 61 418 61 486 C60 548 96 597 145 626 C200 665 275 687 362 687 C444 687 530 665 573 626 C600 601 602 560 573 537 C548 514 504 505 458 499 C398 490 360 476 329 443 C302 415 288 389 248 373 C230 365 211 360 194 360 Z'
path=f'<path fill="#2E2A26" d="{d}"/>'
def svg(w,h,body,title):return f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}"><title>{title}</title>{body}</svg>\n'
(root/'logo/mark.svg').write_text(svg(800,800,path,'Trellis'))
(root/'logo/mark-inverse.svg').write_text(svg(800,800,'<path fill="#2E2A26" d="M0 0H800V800H0Z"/>'+path.replace('#2E2A26','#F4F1E9'),'Trellis inverse'))
# Outlined lettering avoids font substitution in consuming applications.
def lettering(text,size,spacing,cx,baseline):
 buf=io.BytesIO();surface=cairo.SVGSurface(buf,660,940);c=cairo.Context(surface)
 c.select_font_face('Liberation Sans',cairo.FONT_SLANT_NORMAL,cairo.FONT_WEIGHT_NORMAL);c.set_font_size(size)
 widths=[c.text_extents(ch).x_advance for ch in text];x=cx-(sum(widths)+spacing*(len(text)-1))/2
 for ch,w in zip(text,widths):c.move_to(x,baseline);c.text_path(ch);x+=w+spacing
 c.set_source_rgb(46/255,42/255,38/255);c.fill();surface.finish()
 return ''.join(re.findall(r'<path[^>]+/>',buf.getvalue().decode()))
body='<path fill="#F4F1E9" d="M0 0H660V940H0Z"/>'
body+='<g transform="translate(120 132) scale(.5)">'+path+'</g>'
body+=lettering('TRELLIS',106,18,330,640)
body+=lettering('MAP KNOWLEDGE.',25,11,330,734)
body+=lettering('MAKE PROGRESS.',25,11,330,778)
(root/'logo/lockup.svg').write_text(svg(660,940,body,'Trellis — Map knowledge. Make progress.'))
for name in ['mark','mark-inverse','lockup']:
 subprocess.run(['rsvg-convert','-b','#F4F1E9',str(root/f'logo/{name}.svg'),'-o',str(root/f'verification/{name}-render.png')],check=True)
# Export opaque ivory-backed favicon and the exact master-path size ramp.
favicon=svg(800,800,'<path fill="#F4F1E9" d="M0 0H800V800H0Z"/>'+path,'Trellis favicon')
(root/'logo/favicon.svg').write_text(favicon)
for n in (16,32,48,64,128,256,512,1024):
 subprocess.run(['rsvg-convert','-b','#F4F1E9','-w',str(n),'-h',str(n),str(root/'logo/mark.svg'),'-o',str(root/f'logo/mark-{n}.png')],check=True)
