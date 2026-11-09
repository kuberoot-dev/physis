# Draws the article's figures as standalone SVG with the standard library.
import csv, datetime, html, json, math, os, random
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
S1, S2, S3, S4 = "#2a78d6", "#eb6834", "#1baf7a", "#eda100"
INK, INK2, GRID, BG = "#0b0b0b", "#52514e", "#e4e3df", "#fcfcfb"
GRID_INK = "#8a8883"
FONT = "font-family='Inter,Helvetica,Arial,sans-serif'"

def svg(w, h, body):
    return f"<svg xmlns='http://www.w3.org/2000/svg' width='{w}' height='{h}' viewBox='0 0 {w} {h}'><rect width='{w}' height='{h}' fill='{BG}'/>{body}</svg>"

def text(x, y, s, size=13, color=INK, anchor="start", weight="400"):
    return f"<text x='{x:.1f}' y='{y:.1f}' {FONT} font-size='{size}' fill='{color}' text-anchor='{anchor}' font-weight='{weight}'>{html.escape(s)}</text>"

def policies():
    rows = list(csv.DictReader(open(f"{ROOT}/results/sim/2026q3/main/table.csv")))
    base = rows[0]
    names = {"k8s-today": "Kubernetes сегодня", "carbon-aware-cpu": "Углеродно-осознанная, CPU",
             "hetero-fastest": "Разная физика, скорость", "physis-energy": "physis: мин. энергии",
             "physis-carbon": "physis: мин. углерода", "oracle-carbon": "Оракул"}
    W, left, right, top, bh, gap = 900, 250, 40, 96, 14, 2
    rows = [r for r in rows if r["policy"] in names]
    H = top + len(rows) * (2 * bh + gap + 22) + 50
    plot = W - left - right
    b = [text(20, 30, "Энергия и углерод против «Kubernetes сегодня» (= 100), III квартал 2026", 16, weight="600"),
         text(20, 52, "Среднее по пяти прогонам, около 13 тыс. задач в каждом. «Минимум денег» не показан: 3537 и 527.", 12, INK2)]
    for v in range(0, 121, 20):
        x = left + plot * v / 120
        b.append(f"<line x1='{x:.1f}' y1='{top-6}' x2='{x:.1f}' y2='{H-40}' stroke='{GRID}' stroke-width='1'/>")
        b.append(text(x, H - 22, str(v), 11, INK2, "middle"))
    y = top
    for r in rows:
        e = 100 * float(r["kwh_mean"]) / float(base["kwh_mean"])
        c = 100 * float(r["kgco2_mean"]) / float(base["kgco2_mean"])
        b.append(text(left - 10, y + bh + 4, names[r["policy"]], 13, INK, "end"))
        for i, (v, col) in enumerate([(e, S1), (c, S2)]):
            yy = y + i * (bh + gap)
            w = max(plot * min(v, 120) / 120, 2)
            b.append(f"<rect x='{left}' y='{yy}' width='{w:.1f}' height='{bh}' rx='3' fill='{col}'/>")
            b.append(text(left + w + 6, yy + bh - 3, f"{v:.0f}", 11, INK2))
        y += 2 * bh + gap + 22
    lx = 20
    b.append(f"<rect x='{lx}' y='64' width='12' height='12' rx='2' fill='{S1}'/>" + text(lx + 18, 74, "энергия", 12, INK2))
    b.append(f"<rect x='{lx+100}' y='64' width='12' height='12' rx='2' fill='{S2}'/>" + text(lx + 118, 74, "углерод", 12, INK2))
    open(f"{ROOT}/docs/img/policies-q3.svg", "w").write(svg(W, H, "".join(b)))

def grid_week():
    start = datetime.datetime(2026, 7, 13, tzinfo=datetime.timezone.utc)
    end = start + datetime.timedelta(days=7)
    zones = [("PL", "Польша", S1), ("DE", "Германия", S2), ("GB", "Великобритания", S3), ("FR", "Франция", S4)]
    W, H, left, right, top, bottom = 900, 420, 60, 130, 70, 50
    pw, ph = W - left - right, H - top - bottom
    ymax = 800
    b = [text(20, 30, "Интенсивность выбросов энергосистем, неделя 13–20 июля 2026", 16, weight="600"),
         text(20, 52, "г CO₂ на кВт·ч, шаг 15 минут. DE, FR, PL — расчёт по генерации Energy-Charts и IPCC AR5; GB — NESO.", 12, INK2)]
    for v in range(0, ymax + 1, 200):
        y = top + ph * (1 - v / ymax)
        b.append(f"<line x1='{left}' y1='{y:.1f}' x2='{left+pw}' y2='{y:.1f}' stroke='{GRID}' stroke-width='1'/>")
        b.append(text(left - 8, y + 4, str(v), 11, INK2, "end"))
    for d in range(8):
        x = left + pw * d / 7
        day = start + datetime.timedelta(days=d)
        if d < 7:
            b.append(text(x + pw / 14, H - 28, day.strftime("%d.%m"), 11, INK2, "middle"))
    for z, label, col in zones:
        pts = []
        last = None
        for row in csv.DictReader(open(f"{ROOT}/data/grid/2026q3/{z}.csv")):
            t = datetime.datetime.fromtimestamp(int(row["unix"]), datetime.timezone.utc)
            if start <= t < end and row["ci_g_per_kwh"]:
                v = float(row["ci_g_per_kwh"])
                x = left + pw * (t - start).total_seconds() / (7 * 86400)
                y = top + ph * (1 - min(v, ymax) / ymax)
                pts.append(f"{x:.1f},{y:.1f}")
                last = (x, y)
        b.append(f"<polyline points='{' '.join(pts)}' fill='none' stroke='{col}' stroke-width='2' stroke-linejoin='round'/>")
        b.append(text(last[0] + 8, last[1] + 4, label, 12, INK))
    open(f"{ROOT}/docs/img/grid-week.svg", "w").write(svg(W, H, "".join(b)))

def line(x1, y1, x2, y2, color=GRID, width=1, extra=""):
    return f"<line x1='{x1:.1f}' y1='{y1:.1f}' x2='{x2:.1f}' y2='{y2:.1f}' stroke='{color}' stroke-width='{width}' {extra}/>"

def dot(x, y, color, r=5):
    return f"<circle cx='{x:.1f}' cy='{y:.1f}' r='{r}' fill='{color}' stroke='{BG}' stroke-width='2'/>"

def ru(v, digits):
    return f"{v:.{digits}f}".replace(".", ",")

def calibration_table_classify():
    # The main calibration's per-run CSV is not in results/, so the classifier rows come from its summary table.
    units = {"нДж": 1e-9, "мкДж": 1e-6, "мДж": 1e-3, "Дж": 1.0}
    rows, section = [], False
    for ln in open(f"{ROOT}/results/calibration-table.md"):
        if ln.startswith("**"):
            section = "Классификация" in ln
        elif section and ln.startswith("| ") and not ln.startswith("| Физика"):
            phys, algo, q, _, e = [c.strip() for c in ln.strip().strip("|").split("|")]
            num, unit = e.split()
            rows.append((phys, algo.split(" / ")[1], float(q.split()[0]), float(num) * units[unit]))
    return rows

def quality_energy():
    groups = {}
    for f in ("calibration-numeric-runs.csv", "calibration-gpu-runs.csv"):
        for r in csv.DictReader(open(f"{ROOT}/results/{f}")):
            if r["class"] == "classify" and r["n"] == "1797":
                groups.setdefault((r["physics"], r["effort"]), []).append((float(r["quality"]), float(r["device_j"])))
    pts = [(p, e, sum(q for q, _ in v) / len(v), sum(j for _, j in v) / len(v)) for (p, e), v in groups.items()]
    pts += calibration_table_classify()
    real = {"cpu", "gpu"}
    W, H, left, right, top, bottom = 900, 700, 70, 30, 96, 56
    pw, ph = W - left - right, H - top - bottom
    lo, hi, qlo, qhi = -8, 5, 0.945, 1.045
    X = lambda j: left + pw * (math.log10(j) - lo) / (hi - lo)
    Y = lambda q: top + ph * (qhi - q) / (qhi - qlo)
    b = [text(20, 30, "Качество и энергия: партия из 1797 рукописных цифр", 16, weight="600"),
         text(20, 52, "Каждая точка — физика с алгоритмом и усилием; качество — доля от точного классификатора. Энергия устройства, без PUE.", 12, INK2)]
    b.append(dot(26, 70, S1) + text(38, 74, "эмуляция по модели устройства", 12, INK2))
    b.append(dot(246, 70, S2) + text(258, 74, "настоящее железо (CPU и GPU Apple M4)", 12, INK2))
    b.append(f"<circle cx='526' cy='70' r='9' fill='none' stroke='{INK}' stroke-width='1.5'/>" + text(542, 74, "выбор планировщика (минимум энергии)", 12, INK2))
    names = {"нДж": -9, "мкДж": -6, "мДж": -3, "Дж": 0, "кДж": 3}
    for d in range(lo, hi + 1):
        x = left + pw * (d - lo) / (hi - lo)
        b.append(line(x, top, x, top + ph))
        unit = max((u for u in names.items() if u[1] <= d), key=lambda u: u[1])
        b.append(text(x, top + ph + 18, f"{10 ** (d - unit[1])} {unit[0]}", 10.5, INK2, "middle"))
    for q in (0.96, 0.97, 0.98, 1.01, 1.02, 1.03, 1.04):
        b.append(line(left, Y(q), left + pw, Y(q)))
        b.append(text(left - 8, Y(q) + 4, ru(q, 2), 11, INK2, "end"))
    b.append(text(left + pw / 2, H - 12, "Энергия устройства на партию (логарифмическая шкала)", 12, INK2, "middle"))
    choice = {0.95: ("hdc d2048",), 0.99: ("pim-dram int8",), 1.0: ("cpu fp64", "gpu fp32")}
    caption = {0.95: "цель 0,95: hdc d2048", 0.99: "цель 0,99: pim-dram int8", 1.0: "цель 1,0: точный расчёт на CPU или GPU"}
    for q in choice:
        b.append(line(left, Y(q), left + pw, Y(q), INK2, 1.2, "stroke-dasharray='5 4'"))
        b.append(text(left - 8, Y(q) + 4, ru(q, 2), 11, INK, "end", "600"))
        b.append(text(left + pw - 4, Y(q) + 16, caption[q], 12, INK, "end", "600"))
    # Label offsets are hand-placed so that neighbouring points stay readable.
    place = {"hdc d512": (8, 4, "start"), "hdc d2048": (-12, -8, "end"), "hdc d8192": (0, -12, "middle"),
             "stochastic l64": (8, 4, "start"), "stochastic l256": (-8, 4, "end"), "stochastic l1024": (-8, 4, "end"),
             "stochastic l4096": (0, -12, "middle"), "pim-dram int8": (8, 16, "start"), "photonic w4": (8, 10, "start"),
             "neuromorphic t8": (0, 18, "middle"), "analog-imc w4": (8, 4, "start"), "neuromorphic t32": (0, 22, "middle"),
             "neuromorphic t128": (2, -26, "end"), "cpu fp64": (-4, -12, "end"), "dataflow int8": (6, -12, "start"),
             "gpu fp32": None, "systolic int8": None, "systolic e4m3": (12, 16, "start"),
             "systolic e2m1": (8, 4, "start"), "organoid s8": (0, -12, "middle"), "organoid s20": (0, 20, "middle")}
    chosen = {n for v in choice.values() for n in v}
    marks, labels = [], []
    for phys, eff, q, j in sorted(pts, key=lambda p: p[0] in real):
        name = f"{phys} {eff}"
        x, y = X(j), Y(q)
        if name in chosen:
            marks.append(f"<circle cx='{x:.1f}' cy='{y:.1f}' r='10' fill='none' stroke='{INK}' stroke-width='1.5'/>")
        marks.append(dot(x, y, S2 if phys in real else S1))
        if place.get(name):
            dx, dy, anchor = place[name]
            labels.append(text(x + dx, y + dy, name if name != "dataflow int8" else "dataflow int8, gpu fp32, systolic int8", 11, INK2, anchor))
    b += marks + labels
    open(f"{ROOT}/docs/img/quality-energy.svg", "w").write(svg(W, H, "".join(b)))

def cpu_vs_gpu():
    runs = json.load(open(f"{ROOT}/results/real-cpu-vs-gpu.json"))
    batches = sorted({r["batch"] for r in runs})
    val = {(r["physics"], r["batch"]): r for r in runs}
    W, H, top, bottom = 900, 380, 110, 50
    ph = H - top - bottom
    b = [text(20, 30, "Классификатор на настоящем CPU и GPU Apple M4", 16, weight="600"),
         text(20, 52, "Медиана пяти прогонов на партию. Маленькие партии GPU проигрывает: время уходит на подготовку, а не на счёт.", 12, INK2),
         f"<rect x='20' y='66' width='12' height='12' rx='2' fill='{S1}'/>" + text(38, 76, "CPU", 12, INK2),
         f"<rect x='90' y='66' width='12' height='12' rx='2' fill='{S2}'/>" + text(108, 76, "GPU", 12, INK2)]
    panels = [("Время устройства, мс", "device_s", 8, 2), ("Энергия устройства, мДж", "device_j", 60, 20)]
    for i, (title, key, vmax, step) in enumerate(panels):
        x0, pw = 60 + i * 440, 380
        b.append(text(x0, top - 14, title, 13, INK, weight="600"))
        for v in range(0, vmax + 1, step):
            y = top + ph * (1 - v / vmax)
            b.append(line(x0, y, x0 + pw, y))
            b.append(text(x0 - 8, y + 4, str(v), 11, INK2, "end"))
        gw = pw / len(batches)
        for k, n in enumerate(batches):
            gx = x0 + k * gw + gw / 2
            b.append(text(gx, top + ph + 18, f"{n} изобр.", 11, INK2, "middle"))
            for s, (phys, col) in enumerate((("cpu", S1), ("gpu", S2))):
                v = val[(phys, n)][key] * 1000
                h = max(ph * v / vmax, 2)
                x = gx - 38 + s * 40
                b.append(f"<rect x='{x:.1f}' y='{top + ph - h:.1f}' width='36' height='{h:.1f}' rx='3' fill='{col}'/>")
                b.append(text(x + 18, top + ph - h - 6, ru(v, 2 if v < 1 else 1), 11, INK2, "middle"))
    open(f"{ROOT}/docs/img/cpu-vs-gpu.svg", "w").write(svg(W, H, "".join(b)))

def partition_timeline():
    def ts(s):
        return datetime.datetime.fromisoformat(s.rstrip("Z")[:26]).replace(tzinfo=datetime.timezone.utc)
    runs = [("Серверы Hetzner, кластер из четырёх узлов: отрезан рабочий узел на ws2", "partition-stream-workshop-ws2-problems.json",
             {"adiabatic-fr", "analog-imc-fr", "cpu-gb"}, "2026-11-08T22:58:03", "2026-11-08T23:00:04"),
            ("Ноутбук, кластер из двух машин: отрезан рабочий узел", "partition-stream-problems.json",
             {"adiabatic-fr", "analog-imc-fr", "cpu-fr", "cpu-gb", "ising-digital-pl", "neuromorphic-pl", "optical-ising-gb"},
             "2026-11-08T17:32:35", "2026-11-08T17:34:35")]
    W, left, right, ph, gap = 900, 60, 190, 190, 80
    pw = W - left - right
    H = 96 + len(runs) * (ph + gap) - gap + 40
    b = [text(20, 30, "Кто забирал работу во время разрыва сети", 16, weight="600"),
         text(20, 52, "Поток из 120 задач, накопленное число захватов по группам узлов. Серым — две минуты без связи.", 12, INK2)]
    for i, (title, f, worker, c0, c1) in enumerate(runs):
        top = 96 + i * (ph + gap)
        claims = sorted((ts(p["status"]["claimedAt"]), p["status"]["claimedBy"] in worker)
                        for p in json.load(open(f"{ROOT}/results/{f}"))["items"])
        t0 = claims[0][0].replace(second=0, microsecond=0)
        t1 = claims[-1][0].replace(second=0, microsecond=0) + datetime.timedelta(minutes=1)
        span = (t1 - t0).total_seconds()
        X = lambda t: left + pw * (t - t0).total_seconds() / span
        ymax = 100
        Y = lambda v: top + ph * (1 - v / ymax)
        cut0, cut1 = ts(c0 + "Z"), ts(c1 + "Z")
        b.append(text(left, top - 14, title, 13, INK, weight="600"))
        b.append(f"<rect x='{X(cut0):.1f}' y='{top}' width='{X(cut1) - X(cut0):.1f}' height='{ph}' fill='{GRID}' opacity='0.8'/>")
        b.append(text((X(cut0) + X(cut1)) / 2, top + 16, "связи нет", 11, INK2, "middle"))
        for v in range(0, ymax + 1, 25):
            b.append(line(left, Y(v), left + pw, Y(v)))
            b.append(text(left - 8, Y(v) + 4, str(v), 11, INK2, "end"))
        m = t0
        while m <= t1:
            b.append(text(X(m), top + ph + 18, m.strftime("%H:%M"), 11, INK2, "middle"))
            m += datetime.timedelta(minutes=1)
        for is_worker, label, col in ((False, "остальные узлы", S1), (True, "отрезанный узел", S2)):
            n, pts = 0, [f"{X(t0):.1f},{Y(0):.1f}"]
            during = 0
            for t, w in claims:
                if w != is_worker:
                    continue
                pts.append(f"{X(t):.1f},{Y(n):.1f}")
                n += 1
                pts.append(f"{X(t):.1f},{Y(n):.1f}")
                during += cut0 <= t <= cut1
            pts.append(f"{X(t1):.1f},{Y(n):.1f}")
            b.append(f"<polyline points='{' '.join(pts)}' fill='none' stroke='{col}' stroke-width='2' stroke-linejoin='round'/>")
            b.append(text(left + pw + 8, Y(n) + 4, f"{label}: {n}", 12, INK))
            b.append(text(left + pw + 8, Y(n) + 19, f"во время разрыва: {during}", 11, INK2))
    open(f"{ROOT}/docs/img/partition-timeline.svg", "w").write(svg(W, H, "".join(b)))

def box(x, y, w, h, color, title, lines, size=13):
    out = [f"<rect x='{x}' y='{y}' width='{w}' height='{h}' rx='8' fill='{color}' fill-opacity='0.10' stroke='{color}' stroke-width='1.5'/>",
           text(x + 14, y + 24, title, 15, INK, weight="600")]
    out += [text(x + 14, y + 46 + k * 18, s, size, INK2) for k, s in enumerate(lines)]
    return "".join(out)

def arrow(pts, label=None, lx=0, ly=0, anchor="middle"):
    d = "M" + " L".join(f"{x},{y}" for x, y in pts)
    out = f"<path d='{d}' fill='none' stroke='{INK2}' stroke-width='1.5' marker-end='url(#arr)'/>"
    return out + (text(lx, ly, label, 12, INK2, anchor) if label else "")

def objects():
    W, H = 900, 560
    b = ["<defs><marker id='arr' viewBox='0 0 10 10' refX='9' refY='5' markerWidth='7' markerHeight='7' orient='auto-start-reverse'>"
         f"<path d='M0,0 L10,5 L0,10 z' fill='{INK2}'/></marker></defs>",
         text(20, 30, "Объекты нового слоя и путь одной работы", 16, weight="600"),
         text(20, 52, "Вместо подов на узлах — запрос результата, план по физике, месту и времени и подписанная квитанция.", 12, INK2)]
    b.append(box(20, 80, 200, 104, S2, "Mandate", ["человек поручает агенту:", "бюджет, зоны, срок,", "уровень автономии"]))
    b.append(box(20, 250, 200, 122, S1, "Problem", ["запрос результата:", "класс, качество, срок,", "бюджет, политика, зоны", "+ план в status"]))
    b.append(arrow([(120, 184), (120, 248)], "допускает", 128, 222, "start"))
    b.append(box(300, 80, 140, 86, GRID_INK, "Прогнозы", ["углерод и цена", "энергосистем"]))
    b.append(box(460, 80, 140, 86, GRID_INK, "Link", ["задержка, Дж/байт,", "юрисдикция пути"]))
    b.append(box(300, 222, 270, 150, S1, "Планировщик результатов", ["алгоритм × усилие × физика", "× подложка × зона × время;", "прогноз времени, энергии,", "углерода и денег; итог — план"]))
    b.append(arrow([(370, 166), (370, 220)]) + arrow([(530, 166), (530, 220)]))
    b.append(arrow([(220, 290), (298, 290)], "задача", 259, 282))
    b.append(arrow([(298, 340), (222, 340)], "план", 259, 332))
    # Substrates grouped by zone, joined by Link objects.
    b.append(f"<rect x='640' y='80' width='240' height='292' rx='8' fill='{S3}' fill-opacity='0.08' stroke='{S3}' stroke-width='1.5' stroke-dasharray='5 4'/>")
    b.append(text(654, 104, "Substrate + агент", 15, INK, weight="600"))
    zones = [(130, "зона FR", ["CPU", "обратимая логика"]), (210, "зона DE", ["фотоника", "GPU"]), (290, "зона GB", ["живые нейроны", "p-биты"])]
    for y, z, chips in zones:
        b.append(text(654, y, z, 12, INK2))
        for k, c in enumerate(chips):
            b.append(f"<rect x='{654 + k * 114}' y='{y + 8}' width='108' height='30' rx='15' fill='{BG}' stroke='{S3}' stroke-width='1.2'/>")
            b.append(text(708 + k * 114, y + 27, c, 11.5, INK, "middle"))
    for y1, y2 in ((176, 200), (256, 280)):
        b.append(line(866, y1, 866, y2, INK2, 1.5, "stroke-dasharray='3 3'"))
        b.append(text(860, (y1 + y2) / 2 + 4, "Link", 11, INK2, "end"))
    b.append(arrow([(572, 300), (638, 300)], "агент", 605, 290) + text(605, 318, "забирает", 12, INK2, "middle"))
    b.append(box(300, 440, 580, 96, S4, "Receipt — цепочка подписанных квитанций", []))
    for k in range(4):
        x = 316 + k * 140
        b.append(f"<rect x='{x}' y='482' width='112' height='40' rx='6' fill='{BG}' stroke='{S4}' stroke-width='1.2'/>")
        b.append(text(x + 56, 499, "результат, хэш", 11, INK2, "middle") + text(x + 56, 514, "пред., подпись", 11, INK2, "middle"))
        if k:
            b.append(arrow([(x - 26, 502), (x - 2, 502)]))
    b.append(arrow([(770, 372), (770, 438)], "считает и подписывает", 762, 410, "end"))
    b.append(arrow([(298, 488), (120, 488), (120, 374)], "результат", 128, 430, "start"))
    b.append(arrow([(450, 438), (450, 374)], "поправки моделей", 458, 410, "start"))
    open(f"{ROOT}/docs/img/objects.svg", "w").write(svg(W, H, "".join(b)))

def cover():
    rnd = random.Random(7)
    W, H, bg = 1200, 630, "#0e1116"
    cols = [S1, S2, S3, S4, "#a77be0"]
    lanes = [96, 198, 300, 402, 504]
    hub_x, hub = 760, (190, 440)
    b = [f"<rect width='{W}' height='{H}' fill='{bg}'/>"]
    # quantum: Bloch spheres with state vectors
    y = lanes[0]
    for k in range(5):
        cx, r = 70 + k * 110, 30
        th, phi = rnd.uniform(0.3, 2.8), rnd.uniform(0, 2 * math.pi)
        vx, vy = cx + r * math.sin(th) * math.cos(phi), y - r * math.cos(th) + 0.3 * r * math.sin(th) * math.sin(phi)
        b.append(f"<circle cx='{cx}' cy='{y}' r='{r}' fill='none' stroke='{cols[4]}' stroke-opacity='0.7' stroke-width='1.3'/>")
        b.append(f"<ellipse cx='{cx}' cy='{y}' rx='{r}' ry='{r * 0.3}' fill='none' stroke='{cols[4]}' stroke-opacity='0.4' stroke-width='1' stroke-dasharray='3 3'/>")
        b.append(f"<line x1='{cx}' y1='{y}' x2='{vx:.1f}' y2='{vy:.1f}' stroke='{cols[4]}' stroke-width='2' stroke-linecap='round'/>")
        b.append(f"<circle cx='{vx:.1f}' cy='{vy:.1f}' r='4' fill='{cols[4]}'/>")
    # photonic: interfering waves
    y = lanes[1]
    for ph, amp, op in ((0, 22, 0.9), (1.3, 18, 0.6), (2.6, 14, 0.4)):
        pts = " ".join(f"{x},{y + amp * math.sin(x / 19 + ph) * math.cos(x / 97):.1f}" for x in range(30, 600, 3))
        b.append(f"<polyline points='{pts}' fill='none' stroke='{cols[0]}' stroke-opacity='{op}' stroke-width='1.6'/>")
    # analog: smooth continuous signal
    y, v, pts = lanes[2], 0.0, []
    for x in range(30, 600, 4):
        v = 0.9 * v + rnd.uniform(-5, 5)
        pts.append(f"{x},{y + v:.1f}")
    b.append(f"<polyline points='{' '.join(pts)}' fill='none' stroke='{cols[2]}' stroke-width='2' stroke-linejoin='round'/>")
    for x in range(40, 600, 28):
        b.append(f"<line x1='{x}' y1='{y + 28}' x2='{x}' y2='{y + 28 - rnd.uniform(4, 18):.1f}' stroke='{cols[2]}' stroke-opacity='0.45' stroke-width='3' stroke-linecap='round'/>")
    # living neurons: somas with branching dendrites
    y = lanes[3]
    def branch(x, yy, ang, length, depth):
        x2, y2 = x + length * math.cos(ang), yy + length * math.sin(ang)
        out = [f"<line x1='{x:.1f}' y1='{yy:.1f}' x2='{x2:.1f}' y2='{y2:.1f}' stroke='{cols[1]}' stroke-opacity='{0.4 + 0.15 * depth:.2f}' stroke-width='{0.6 + 0.5 * depth:.1f}' stroke-linecap='round'/>"]
        if depth:
            for d in (-0.5, 0.45):
                out += branch(x2, y2, ang + d + rnd.uniform(-0.2, 0.2), length * 0.68, depth - 1)
        return out
    for k in range(4):
        cx = 90 + k * 145 + rnd.uniform(-10, 10)
        for a in range(5):
            b += branch(cx, y, a * 2 * math.pi / 5 + rnd.uniform(-0.3, 0.3), 20, 3)
        b.append(f"<circle cx='{cx:.1f}' cy='{y}' r='7' fill='{cols[1]}'/>")
    # silicon: regular grid of cells with traces
    y = lanes[4]
    for r in range(3):
        for c in range(26):
            x = 34 + c * 22
            op = 0.35 + 0.5 * rnd.random()
            b.append(f"<rect x='{x}' y='{y - 30 + r * 22}' width='14' height='14' rx='2' fill='{cols[3]}' fill-opacity='{op:.2f}'/>")
    # every physics flows into one scheduling layer
    for k, ly in enumerate(lanes):
        for j in range(3):
            ty = hub[0] + 20 + (k * 3 + j) * (hub[1] - hub[0] - 40) / 14
            b.append(f"<path d='M600,{ly + (j - 1) * 8} C680,{ly + (j - 1) * 8} 690,{ty:.1f} {hub_x - 4},{ty:.1f}' fill='none' stroke='{cols[[4, 0, 2, 1, 3][k]]}' stroke-opacity='0.55' stroke-width='1.4'/>")
    b.append(f"<rect x='{hub_x}' y='{hub[0]}' width='10' height='{hub[1] - hub[0]}' rx='5' fill='#e8e6e1'/>")
    for k in range(9):
        yy = hub[0] + 30 + k * (hub[1] - hub[0] - 60) / 8
        b.append(f"<line x1='{hub_x + 14}' y1='{yy:.1f}' x2='{hub_x + 40}' y2='{yy:.1f}' stroke='#e8e6e1' stroke-opacity='0.5' stroke-width='1.4' stroke-linecap='round'/>")
    title = "font-family='Inter,Helvetica,Arial,sans-serif'"
    b.append(f"<text x='840' y='318' {title} font-size='92' font-weight='700' fill='#f4f2ee'>physis</text>")
    b.append(f"<text x='843' y='370' {title} font-size='27' fill='#b9b6af'>Инфраструктура</text>")
    b.append(f"<text x='843' y='404' {title} font-size='27' fill='#b9b6af'>для завтрашних</text>")
    b.append(f"<text x='843' y='438' {title} font-size='27' fill='#b9b6af'>вычислений</text>")
    doc = f"<svg xmlns='http://www.w3.org/2000/svg' width='{W}' height='{H}' viewBox='0 0 {W} {H}'>{''.join(b)}</svg>"
    open(f"{ROOT}/docs/img/cover.svg", "w").write(doc)
    try:
        import cairosvg  # optional: python3 -m venv out/venv && out/venv/bin/pip install cairosvg
    except (ImportError, OSError):
        print("cairosvg not available: docs/img/social-card.png left as is")
        return
    cairosvg.svg2png(bytestring=doc.encode(), write_to=f"{ROOT}/docs/img/social-card.png", output_width=W, output_height=H)

policies()
if os.path.isdir(f"{ROOT}/data/grid/2026q3"):
    grid_week()
else:
    print("data/grid/2026q3 not downloaded: docs/img/grid-week.svg left as is")
quality_energy()
cpu_vs_gpu()
partition_timeline()
objects()
cover()
print("ok")
