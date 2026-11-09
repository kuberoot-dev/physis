"""Fetches every URL in docs/references.md and records whether it answers
and what it is titled, so the bibliography can be re-checked at any time.
Writes results/references-check.tsv: key, url, HTTP status, page title.
A 403 means the site refuses automated clients, not that the page is gone;
those pages were opened in a browser (see docs/verification.md)."""
import re, sys, urllib.request, html, concurrent.futures as cf
refs = open('docs/references.md').read()
rows = []
for m in re.finditer(r'^- \*\*([\w.-]+)\*\* —(.*)$', refs, re.M):
    for u in re.findall(r'https?://[^\s;)]+', m.group(2)):
        rows.append((m.group(1), u.rstrip('.,')))
def fetch(r):
    k, u = r
    try:
        req = urllib.request.Request(u, headers={'User-Agent': 'Mozilla/5.0 (Macintosh) reference-check'})
        with urllib.request.urlopen(req, timeout=30) as f:
            b = f.read(400000).decode('utf-8', 'ignore')
            t = re.search(r'<title[^>]*>(.*?)</title>', b, re.S | re.I)
            title = html.unescape(re.sub(r'\s+', ' ', t.group(1))).strip()[:140] if t else ('(pdf)' if b.startswith('%PDF') else '')
            return k, u, f.status, title
    except urllib.error.HTTPError as e:
        return k, u, e.code, ''
    except Exception as e:
        return k, u, 'ERR', type(e).__name__
with cf.ThreadPoolExecutor(12) as ex:
    out = list(ex.map(fetch, rows))
with open('results/references-check.tsv', 'w') as f:
    f.write('key\turl\tstatus\ttitle\n')
    for r in out:
        f.write('\t'.join(map(str, r)) + '\n')
bad = [r for r in out if r[2] != 200]
print(f'{len(out)} URLs, {len(out)-len(bad)} answered 200')
for r in bad:
    print(*r[:3], sep='\t')
