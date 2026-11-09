"""Runs a linear classifier on the Apple GPU through MLX for physis-agent.

Reads {"w": [[...]], "b": [...], "x": [[...]]} as JSON on stdin and writes
{"pred": [...], "setupSeconds": s, "seconds": t, "device": "..."} to stdout:
s is the first evaluation (Metal kernel compilation), t the wall time of the
classification on the GPU. The interpreter start and the import are not
counted. The physis agent
starts it once per problem: GOOS=darwin agents with -physics gpu.
"""
import json, sys, time
import mlx.core as mx

req = json.load(sys.stdin)
mx.set_default_device(mx.gpu)
w = mx.array(req["w"], dtype=mx.float32)
b = mx.array(req["b"], dtype=mx.float32)
x = mx.array(req["x"], dtype=mx.float32)
mx.eval(w, b, x)  # move inputs first: the run measures computation
# The first evaluation compiles the Metal kernels; it is reported apart as
# setup, the way a device model separates loading from computing.
t0 = time.perf_counter()
mx.eval(mx.argmax(x[:1] @ w.T + b, axis=1))
t1 = time.perf_counter()
pred = mx.argmax(x @ w.T + b, axis=1)
mx.eval(pred)
t2 = time.perf_counter()
json.dump({"pred": pred.tolist(), "setupSeconds": t1 - t0, "seconds": t2 - t1,
           "device": str(mx.default_device())}, sys.stdout)
