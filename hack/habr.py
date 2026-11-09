"""Builds the Habr edition of docs/article.ru.md.

Habr takes the title in a separate field and renders neither SVG nor
citation keys, so the edition differs from the repository text only in form:
  - the title line loses its '#', sections become '#', subsections '##';
  - SVG figures become PNG (rendered with rsvg-convert at 2x), GIFs stay;
  - citation keys [key] become numbers [N] in order of first use, and the
    bibliography becomes a numbered list in that order (entries never cited
    in the text are kept after them).
Output: <out>/article-habr.md and <out>/img/*.png|gif, ready to paste and
upload.
  python3 hack/habr.py [out-dir]   (default ~/Downloads/physis-habr)
"""
import os, re, shutil, subprocess, sys

src = 'docs/article.ru.md'
out = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser('~/Downloads/physis-habr')
os.makedirs(os.path.join(out, 'img'), exist_ok=True)
text = open(src, encoding='utf-8').read()

lit = text.index('\n## Литература и данные')
body, bib = text[:lit], text[lit:]

# Bibliography entries: "- **key** — ..."
entries = {}
order = []
for m in re.finditer(r'^- \*\*([\w.-]+)\*\* — (.*)$', bib, re.M):
    entries[m.group(1)] = m.group(2)
    order.append(m.group(1))

num = {}
def number(key):
    if key not in num:
        num[key] = len(num) + 1
    return num[key]

def cite(m):
    keys = [k.strip() for k in m.group(1).split(',')]
    if not all(k in entries for k in keys):
        return m.group(0)  # not a citation (a link label, a code index...)
    return '[' + ', '.join(str(number(k)) for k in keys) + ']'

# Leave code spans and blocks alone.
parts = re.split(r'(```.*?```|`[^`\n]*`)', body, flags=re.S)
for i in range(0, len(parts), 2):
    parts[i] = re.sub(r'\[([a-z][\w.,\s-]*)\](?!\()', cite, parts[i])
body = ''.join(parts)

# Headings: title without '#', then one level up.
lines = body.split('\n')
for i, l in enumerate(lines):
    if i == 0 and l.startswith('# '):
        lines[i] = l[2:]
    elif l.startswith('#### '):
        lines[i] = '### ' + l[5:]
    elif l.startswith('### '):
        lines[i] = '## ' + l[4:]
    elif l.startswith('## '):
        lines[i] = '# ' + l[3:]
body = '\n'.join(lines)

# Figures.
def figure(m):
    alt, path = m.group(1), m.group(2)
    name = os.path.basename(path)
    srcp = os.path.join('docs', path)
    if name.endswith('.svg'):
        png = name[:-4] + '.png'
        subprocess.run(['rsvg-convert', '-z', '2', '-b', 'white', '-o', os.path.join(out, 'img', png), srcp], check=True)
        name = png
    else:
        shutil.copy(srcp, os.path.join(out, 'img', name))
    return f'![{alt}](img/{name})'
body = re.sub(r'!\[([^\]]*)\]\((img/[^)]+)\)', figure, body)

# Numbered bibliography: cited entries in order of first use, then the rest.
cited = sorted(num, key=num.get)
rest = [k for k in order if k not in num]
refs = ['# Литература и данные', '',
        'Номера в квадратных скобках в тексте ведут сюда. Полный список с пометками о том, что мы проверяли сами, и все данные — в репозитории проекта: https://github.com/kuberoot-dev/physis', '']
for k in cited:
    refs.append(f'{num[k]}. {entries[k]}')
if rest:
    refs += ['', 'Источники, которые в тексте не цитируются, но использовались как фон:', '']
    refs += [f'- {entries[k]}' for k in rest]
open(os.path.join(out, 'article-habr.md'), 'w', encoding='utf-8').write(body.rstrip() + '\n\n' + '\n'.join(refs) + '\n')
print(f'{out}/article-habr.md: {len(cited)} cited sources, {len(rest)} uncited; figures in {out}/img')
