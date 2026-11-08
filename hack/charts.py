# Draws the article's figures as standalone SVG with the standard library.
import csv, datetime, html, os
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
S1, S2, S3, S4 = "#2a78d6", "#eb6834", "#1baf7a", "#eda100"
INK, INK2, GRID, BG = "#0b0b0b", "#52514e", "#e4e3df", "#fcfcfb"
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

policies()
grid_week()
print("ok")
