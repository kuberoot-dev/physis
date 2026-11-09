# Coverage map

Where each idea from the preparatory discussion lives in the article
(`docs/article.ru.md`) and, where it was tested, in the prototype. "Tested"
means a result in `results/` or a conformance test; "design" means argued, not
implemented.

| Idea | Article section | Status |
|---|---|---|
| Energy as the binding constraint; grid, heat, carbon, price per hour | 1, F1 | design + scheduler objectives (tested, 8.2) |
| Materials limits: copper, transformers, turbines, helium; oxygen is not a limit | 1, F1 | design |
| Divergent physics of computation (analog, Ising, p-bits, optics, SFQ, adiabatic, neuromorphic, quantum, living cultures, DNA storage) | 1, F2; 7 "Двенадцать физик" | CPU and GPU (Apple M4) real and measured; ten emulated and calibrated (7, 8) |
| Non-copyable device state; migration means retraining; care of devices; ethics of living hardware | 1, F2 | design |
| Architectures that change how numbers are represented or where they are computed (systolic, dataflow, PIM, FP8/MXFP4, stochastic, hyperdimensional, CGRA) | 1, F2; 8.6 | five emulated with measured accuracy; conformance 17/17 |
| Dissolving machine boundaries (CXL, optical interconnect, rack-scale) | 1, F3 | design |
| Agents as the main users; defensive server-side code | 1, F4; 3 | design; budgets and mandates enforced server-side (TC-06, TC-12) |
| Trust is no longer free; verification levels; receipts | 1, F5; 3 | tested (TC-10, TC-11) |
| Transparency log for receipts (CT / Rekor) | 3 | design |
| Compute as a commodity; market roles; capture risk | 1, F6; 2 | design |
| Limits of a single interface: lowest common denominator, embrace-extend, data gravity, commoditise the complement | 2 | design |
| Jevons paradox | 1, F6 | design |
| Disconnected and delay-tolerant operation | 1, F7; 6 "Сеть"; 8.6 | tested (TC-14; real link cut on a two-node kuberoot cluster) |
| Systems designing machines (ADRS) | 1, F8; 6 "Планирование" | design |
| State over computation (DBOS, Hydro, CALM, local-first) | 1, F9 | design |
| Uncertainty as the norm; quality as a bounded probability | 1, F10; 7 | tested (TC-05, TC-16) |
| IFTF and FTSG forecasts; human/agent split | 3 | design |
| Amara's law; maturity ladder from practical to radical | 1 | design |
| Ackley robust-first computing | 6 "Планирование" | design |
| Principles P1–P14 | 4; `principles.md` | P2–P5, P7–P12, P14 have conformance tests; P1, P6, P13 are design only |
| What stays in Kubernetes (API machinery, reconciliation, CRDs) | 5 | used by the prototype |
| Node as the wrong abstraction; Linux binding is a node property (kcp, virtual-kubelet); minimal OS (Talos, Hubris) | 5 | tested: no Node objects, no node RBAC (TC-02) |
| Network stack audit (flat pod IP, kube-proxy, CNI, Service) | 5 "Сетевой стек подробно" | design |
| Network redesign: priced Link objects, jurisdictions on paths, data before start | 6 "Сеть" | tested (TC-17, 8.3) |
| Multi-level problem IR; the scheduler chooses the model of computation | 6 "Планирование" | partly: algorithm × physics search (8.2) |
| Quantum scheduling in hybrid clusters | 6 | QPU emulated (7) |
| Cloud as a market of substrates; sovereignty, jurisdictions | 6 | tested (TC-07) |
| Compatibility with today's Kubernetes | 6 "Совместимость" | runs on kube-apiserver 1.37 and kuberoot |
| Cozystack as the open reference implementation; kuberoot as the laboratory | 11 | design |
| Carbon-, energy- and price-aware scheduling with deferral | 7, 8 | tested (8.2, 8.5, TC-09) |
| Learning device models from receipts | 8.4 | tested |
| Conformance for tomorrow's systems | 9; `conformance.md` | 17/17 on two stands, control 0/17 |
