# How the article was checked

This is the record of how the claims in `docs/article.ru.md` were verified,
what was found wrong and what could not be checked automatically. It is meant
to be repeatable: the scripts named here run again on any machine.

## 1. Every claim, line by line

The article was split into three parts, and each part was checked
independently of the authors' reasoning:

| Part | What was checked | Claims checked | Corrections |
|---|---|---|---|
| Sections 1–3 (forces, economics, people and trust) | every external fact, number, date and attribution, against the cited source, fetched | about 80 | about 30 |
| Sections 4–6 (principles, Kubernetes audit, design) | every statement about Kubernetes and other systems against official documentation; every statement about the prototype against the code | about 80 | about 17 |
| Sections 7–11 (prototype, evaluation, conformance, limitations) | every number and table cell against `results/` and the code, recomputing derived percentages; the models table regenerated from code and diffed | about 240 | about 19 |

The corrections are in the history of `docs/article.ru.md` (commits named
"fix facts found by the review of sections ..."). The main kinds of error
found, so that a reader knows what to watch for in work like this:

- a figure attributed to the wrong scope (Borg's machines, the Aslan transport
  energy figure, the Kubernetes scaling limits);
- a citation that did not say what the text said (IFTF 2036 themes, the Data
  Act, Homa, ADRS, the Vaire and Loihi figures, a 740 W/W cryogenic factor);
- an overstatement of the prototype (a DRA bridge and a hierarchical planner
  that exist only in the design; "6405 runs" that are 6405 calibration
  records; a deadline miss reported as none);
- numbers derived wrongly (a "thousands of times" that is a million; savings
  of 2–40× reported as orders of magnitude).

Where the prototype claimed something it did not test, a test was added: the
carbon of moved data (`pkg/plan/plan_test.go`).

## 2. Every link

`hack/check-references.py` fetches every URL in `docs/references.md` and
writes `results/references-check.tsv` (status and page title). On the last
run 83 of 90 URLs answered directly. The rest:

| Key | Why not directly | How it was checked |
|---|---|---|
| gartner2026 | the site refuses automated clients (403) | opened in a browser on 8 Nov 2026: the release title matches the text ($6.37 trillion, +14.2%) |
| suncatcher (fierce-network) | 403 to automated clients | opened in a browser: Starcloud-1 with an NVIDIA H100; Suncatcher's "two prototype satellites by early 2027" |
| metamaterial2026 (Wiley) | 403 to automated clients | opened in a browser: Yin, Mei, Chen, *Advanced Materials* 2026, "mechanical analog-to-digital conversion ... FPGA-like logic operations"; DOI also confirmed through Crossref |
| iftf2036, iftf2025 | iftf.org did not answer from our network on the last run | the pages were fetched on 8 Nov 2026 and checked for the cited statements ("Discernment", "The Future of Scientific Knowledge", "Four Cultures of AI", "What We Choose Next", "The Human Verification Crisis") |
| ipcc2014 (NREL link) | nrel.gov did not answer from our network | the factors are checked against the primary instead, IPCC AR5 WGIII Annex III, Table A.III.2 (fetched from ipcc.ch); see below |

Sources that could not be verified were replaced or removed rather than kept:
a Forbes article (403, no reachable copy), a Coinbase page (403, superseded by
the Linux Foundation announcement), an AI-written page about Cortical Labs,
and a Neowin page behind a bot check.

## 3. Numbers that feed the model

- **Life-cycle emission factors** (`pkg/grid/fetch.go`) were compared with
  Table A.III.2 of the IPCC Annex III: coal 820, gas 490, biomass 230,
  geothermal 38, hydro 24, nuclear 12, utility PV 48, offshore wind 12 match.
  Onshore wind is 11 in the table and 11.5 in the code — a transcription slip,
  kept so that the published results reproduce, and noted in the code and the
  references; it moves intensities by at most 0.5 g/kWh.
- **Device parameters** carry their provenance in the code
  (`measured`, `ref:<key>` or `assumption`); the models table in the article is
  generated from it (`go run ./cmd/physis-models`). Labels were corrected
  in the check: the adiabatic energy per operation is an assumption, with only
  the 30% saving taken from the source. The 4 K cryogenic factor of 740 W/W was
  first relabelled as an assumption, then restored to its source when a later
  check found the figure in the SPIE abstract.
- **Results** in sections 8–9 are recomputed from the files in `results/`.

## 4. Behaviour

The prototype's claims about itself are checked by the conformance suite
(`docs/conformance.md`) on four stands, by unit tests (`go test ./...`), and by
network-cut experiments with their raw data in `results/partition-*`. The
conformance runs found bugs that are described in the article; each was fixed
and the suite run again.

## 5. A last pass over late edits

Text added or rewritten after the three reviews (the multi-node stands, new
references, the corrections themselves) was checked once more, independently:
about 95 claims, 18 corrections — among them two experiments credited to the
wrong cluster size, an out-of-date count of bugs, a reversed statement about
lenders, a wrong alpha release of nftables, and the cryogenic factor above.

The anniversary of the IFTF forecast was dropped: IFTF's own page speaks of
the "50th year of this program" in 2025, other sources date the forecast to
1978, and the article does not need the number.
