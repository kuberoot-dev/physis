"""Throughput and setup of tools/gpu-worker.py on the Apple GPU for growing
batches (synthetic 64-feature inputs, a 10x64 model). Three runs per size,
medians. Run with the MLX venv: out/venv/bin/python -I hack/gpu-benchmark.py"""
import json, random, statistics, subprocess, sys
random.seed(1)
w = [[random.random() for _ in range(64)] for _ in range(10)]
b = [0.0] * 10
for n in (1797, 50000, 200000):
    x = [[random.randint(0, 16) for _ in range(64)] for _ in range(n)]
    ts = []
    for _ in range(3):
        p = subprocess.run([sys.executable, '-I', 'tools/gpu-worker.py'], input=json.dumps({"w": w, "b": b, "x": x}), capture_output=True, text=True, check=True)
        r = json.loads(p.stdout)
        ts.append((r['setupSeconds'], r['seconds']))
    s = statistics.median(t[0] for t in ts); c = statistics.median(t[1] for t in ts)
    print(f"{n} setup {s:.4f} s, compute {c:.5f} s, {n*640/c:.3g} MAC/s")
