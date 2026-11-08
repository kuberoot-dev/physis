# References

Keys are used in code comments (`ref:key`) and in the documents. Each entry
gives the primary source where we found one and, where a figure was taken from
a secondary source, says so. "Checked" notes what we verified ourselves.

## Physics and devices

- **legallo2023** — M. Le Gallo et al., "A 64-core mixed-signal in-memory compute chip based on phase-change memory for deep neural network inference", *Nature Electronics* (2023), doi:10.1038/s41928-023-01010-1. 63.1 TOPS at 9.76 TOPS/W for 8-bit input/output MVM; effective MVM precision 3–4 bits. HERMES, 63.1 TOPS and 9.76 TOPS/W are in the abstract; the 3–4-bit precision is in the body text (arXiv:2212.02872); see also HPCwire's summary: https://www.hpcwire.com/off-the-wire/ibm-develops-a-new-64-core-mixed-signal-in-memory-computing-chip/
- **ambrogio2023** — S. Ambrogio et al., "An analog-AI chip for energy-efficient speech recognition and transcription", *Nature* 620 (2023). Up to 12.4 TOPS/W. https://www.nature.com/articles/s41586-023-06337-5
- **horowitz2014** — M. Horowitz, "Computing's energy problem (and what we can do about it)", ISSCC 2014, doi:10.1109/ISSCC.2014.6757323. Energy per operation at 45 nm (8-bit multiply ≈ 0.2 pJ, 8-bit add ≈ 0.03 pJ). Copy: https://www.Gwern.net/doc/cs/hardware/2014-horowitz-2.pdf (we did not re-read the table in this work; the values are the widely cited ones).
- **singh2024** — N. S. Singh et al., CMOS plus stochastic magnetic tunnel junctions for probabilistic computing (arXiv:2304.05949). The paper does not measure sMTJ p-bit energy; it repeats an earlier projection of about 2 fJ per random bit (its Ref. [28]), and reports 145–293 fJ for CMOS p-bits. https://arxiv.org/html/2304.05949v3
- **aadit2022** — N. A. Aadit et al., "Massively parallel probabilistic computing with sparse Ising machines", *Nature Electronics* (2022). Background for p-bit throughput; no figure is taken from it.
- **tatsumura2019** — K. Tatsumura, A. Dixon, H. Goto, "FPGA-Based Simulated Bifurcation Machine", FPL 2019: 2000-node all-to-all MAX-CUT 14× faster than the CIM and 124× faster than optimised simulated annealing (paper, doi:10.1109/FPL.2019.00019). Slides: https://fpl2019.bsc.es/sites/default/files/uploaded/A12.FPL2019-slides-FPGABasedSimulatedBifurcation.pdf ; algorithm: H. Goto, K. Tatsumura, A. Dixon, *Science Advances* (2019), doi:10.1126/sciadv.aav2372.
- **cim2018** — NTT, coherent Ising machine with up to 1936 coupled DOPOs, *Nature Communications* 9 (2018). https://ideas.repec.org/a/nat/natcom/v9y2018i1d10.1038_s41467-018-07328-1.html
- **cim2026** — Optical Ising machine based on polarization symmetry breaking in a driven Kerr resonator (Auckland/Dijon/Strathclyde), *Nature Communications* (2026). https://thequantuminsider.com/2026/03/24/new-zealand-researchers-report-advance-in-optical-ising-machine-for-hybrid-quantum-optimization/
- **arute2019supp** — F. Arute et al., supplementary information for "Quantum supremacy using a programmable superconducting processor" (arXiv:1910.11333): total worst-case apparatus power about 26 kW (dilution refrigerator about 10 kW plus chilled water, about 3 kW electronics). https://arxiv.org/pdf/1910.11333
- **cryo4k** — Development of a 4 K–10 K Collins-type cryocooler for space, SPIE 5904 (2005): small coolers of about 1 W at 4 K "typically require 5 kW–7.5 kW per Watt". https://opticalengineering.spiedigitallibrary.org/conference-proceedings-of-spie/5904/59040X/Development-of-a-4K-10K-Collins-type-cryocooler-for-space/10.1117/12.626151.full
- **imec-sfq** — imec superconducting digital computing: "How to Put a Data Center in a Shoebox", IEEE Spectrum. https://spectrum.ieee.org/superconducting-computer
- **loihi** — M. Davies et al., "Loihi: A Neuromorphic Manycore Processor with On-Chip Learning", *IEEE Micro* 38(1), 2018. Energy per synaptic operation as reported by later surveys: 20–25 pJ (we did not find the figure in the paper's abstract). https://arxiv.org/pdf/2509.23896
- **halapoint** — Sandia, Hala Point (1152 Loihi 2 chips, 1.15 billion neurons). https://www.sandia.gov/news/publications/hpc-annual-reports/article/advancing-neuromorphic-computing-at-the-neural-exploration-and-research-lab/
- **photonic2024** — Minimalistic photonic matrix processor: 0.27 pJ/MAC laser energy at 10 mW and 36.7 GBaud (arXiv:2403.04216). https://arxiv.org/pdf/2403.04216
- **lightmatter** — Lightmatter photonic interconnect roadmap. https://convergedigest.com/ai-infrastructure-summit-lightmatters-photonic-roadmap/?amp=1
- **cl1power** — Cortical Labs CL1: hundreds of thousands of lab-grown neurons on a silicon chip with a 59-electrode array, about $35,000 per unit, neurons kept alive up to six months; 850–1000 W quoted for a rack of 30 units (Neowin) or per unit (DataCenterDynamics) — the sources disagree. We take about 30 W of life support per unit; a higher figure only makes the culture less attractive, which does not change any result. https://heise.de/-10312614 ; https://datacenterdynamics.com/en/news/australian-startup-cortical-labs-unveils-worlds-first-commercial-biological-computer ; https://www.neowin.net/amp/this-is-an-actual-living-computer-sort-of-that-runs-on-neither-windows-nor-linux/
- **finalspark** — FinalSpark Neuroplatform; organoid lifetime about 100 days. https://www.tomshardware.com/pc-components/cpus/worlds-first-bioprocessor-uses-16-human-brain-organoids-for-a-million-times-less-power-consumption-than-a-digital-chip
- **vaire** — Vaire Computing adiabatic reversible test chip "Ice River": tested in August 2025, it "drew around 30 percent less energy than a regular chip performing the same computation" (Science News). https://www.sciencenews.org/article/computer-chip-reuses-energy ; https://www.techradar.com/pro/more-like-a-pendulum-than-a-hammer-the-ai-chip-that-can-reuse-its-own-energy-reaches-proof-of-concept-stage-but-i-dont-think-it-will-be-enough-to-convince-hyperscalers-to-invest
- **extropic** — Extropic thermodynamic computing. https://extropic.ai/writing/from-one-to-one-billion
- **mcmahon** — L. G. Wright et al., deep physical neural networks trained with backpropagation, *Nature* 601 (2022); training review *Nature* 645 (2025). https://mcmahon.aep.cornell.edu/aspen/2024/slides/Wright.pdf
- **metamaterial2026** — Mechanical metamaterials with reprogrammable sequential deformation for analog-to-digital encoding and computing, *Advanced Materials* (2026). https://advanced.onlinelibrary.wiley.com/doi/10.1002/adma.74376
- **ackley** — D. Ackley, robust-first computing and the Movable Feast Machine. https://highscalability.com/pursue-robust-indefinite-scalability-with-the-movable-feast/ ; T2 Tiles: https://creativegood.com/blog/21/ackley-and-tiles.html
- **landauer** — the Landauer limit kT ln 2 per erased bit (textbook result).
- **pcss-hybrid** — Hybrid classical-quantum supercomputing: multi-user, multi-QPU and multi-GPU environment at PCSS under Slurm (arXiv:2508.16297). https://arxiv.org/pdf/2508.16297
- **hpc-qc-sched** — Assessing the elephant in the room in scheduling for current hybrid HPC-QC clusters (arXiv:2504.10520). https://arxiv.org/pdf/2504.10520
- **dna2026** — Biomemory DNA storage in datacenters (Blocks & Files, 2026). https://www.blocksandfiles.com/architecture/2026/03/06/french-dna-coming-to-a-datacenter-near-you-soon/4093753
- **suncatcher** — Google Project Suncatcher: two prototype TPU satellites with Planet Labs by early 2027 (Interesting Engineering); Starcloud satellite with an NVIDIA GPU. https://interestingengineering.com/innovation/google-project-suncatcher-space-ai ; https://www.fierce-network.com/cloud/space-data-centers-starcloud-spacex-and-project-suncatcher-explained

## Systems and software

- **borg** — A. Verma et al., "Large-scale cluster management at Google with Borg", EuroSys 2015.
- **k8s-large** — Kubernetes, "Considerations for large clusters": designed for at most 110 pods per node, 5,000 nodes, 150,000 pods and 300,000 containers. https://kubernetes.io/docs/setup/best-practices/cluster-large/
- **k8s-net** — Kubernetes cluster networking model: every pod has its own IP, pods communicate without NAT. https://kubernetes.io/docs/concepts/cluster-administration/networking/
- **nftables** — KEP-3866, "Add an nftables-based kube-proxy backend": alpha 1.31, stable 1.33; iptables remains the default. https://www.k8s.dev/resources/keps/3866/ ; "NFTables mode for kube-proxy", Kubernetes blog, 2025. https://kubernetes.io/blog/2025/02/28/nftables-kube-proxy/
- **kep5495** — KEP-5495: deprecate ipvs mode in kube-proxy (deprecated in 1.35). https://www.k8s.dev/resources/keps/5495/
- **dra** — "Kubernetes v1.34: DRA has graduated to GA", Kubernetes blog, 2025. https://kubernetes.io/blog/2025/09/01/kubernetes-v1-34-dra-updates/ ; documentation: https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/
- **homa** — J. Ousterhout, "It's Time to Replace TCP in the Datacenter", arXiv:2210.00714. https://arxiv.org/abs/2210.00714
- **rfc9171** — S. Burleigh, K. Fall, E. Birrane, "Bundle Protocol Version 7", RFC 9171, 2022. https://www.rfc-editor.org/rfc/rfc9171
- **dbos** — "DBOS: A DBMS-oriented Operating System", PVLDB 15. https://mail.vldb.org/pvldb/volumes/15/paper/DBOS%3A%20A%20DBMS-oriented%20Operating%20System
- **hydro** — J. Hellerstein, Hydro. https://jhellerstein.github.io/blog/hello-aws
- **calm** — J. Hellerstein, P. Alvaro, "Keeping CALM: When Distributed Consistency Is Easy", *CACM* 2020.
- **adrs** — "Barbarians at the Gate: How AI is Upending Systems Research", arXiv:2510.06189. https://arxiv.org/abs/2510.06189v1
- **demoura2026** — L. de Moura, "Lean 4: A Unified Platform for Programming and Verification", NFM 2026. https://leodemoura.github.io/static/nfm2026/
- **skypilot** — "Going Beyond Hyperscaler Clouds in the Sky Ecosystem with SkyPilot". https://www2.eecs.berkeley.edu/Pubs/TechRpts/2023/EECS-2023-179.html
- **localfirst** — M. Kleppmann, local-first software (SE Radio 716, 2026). https://se-radio.net/2026/04/se-radio-716-martin-kleppmann-local-first-software/
- **malleable** — Ink & Switch, "Malleable Software", 2025. https://www.inkandswitch.com/essay/malleable-software/
- **microvm2026** — "Your Container Is Not a Sandbox: The State of MicroVM Isolation in 2026". https://emirb.github.io/blog/microvm-2026/
- **oxide** — Oxide Computer: a rack designed as one integrated platform (compute, storage, networking, software). https://oxide.computer/
- **kcp** — kcp: "Kubernetes-like control planes for form-factors and use-cases beyond Kubernetes and container workloads"; it serves Kubernetes-style APIs with no nodes. https://github.com/kcp-dev/kcp
- **virtual-kubelet** — Virtual Kubelet: a kubelet implementation that "masquerades as a kubelet for the purposes of connecting Kubernetes to other APIs" (ACI, AWS Fargate and others). https://github.com/virtual-kubelet/virtual-kubelet
- **talos** — Talos Linux: an immutable, API-managed OS that runs only Kubernetes. https://www.talos.dev/
- **hubris** — Oxide Computer, Hubris: a small open-source operating system for deeply-embedded systems (the microcontrollers of Oxide's racks). https://hubris.oxide.computer/
- **rfc9162** — B. Laurie, E. Messeri, R. Stradling, "Certificate Transparency Version 2.0", RFC 9162, 2021. https://www.rfc-editor.org/rfc/rfc9162
- **rekor** — Sigstore Rekor, an append-only transparency log for signed software artefacts. https://docs.sigstore.dev/logging/overview/
- **mlir** — C. Lattner et al., "MLIR: Scaling Compiler Infrastructure for Domain Specific Computation", CGO 2021; preprint "MLIR: A Compiler Infrastructure for the End of Moore's Law", arXiv:2002.11054. https://arxiv.org/abs/2002.11054
- **jevons** — W. S. Jevons, *The Coal Question*, 1865; on the rebound effect see B. Alcott, "Jevons' paradox", *Ecological Economics* 54 (2005), doi:10.1016/j.ecolecon.2005.03.020. Checked: title, author, journal and volume via Crossref.
- **cxl-dc** — Panmnesia and Meta, CXL fabric spanning AI datacentres. https://www.hpcwire.com/off-the-wire/panmnesia-and-meta-outline-cxl-fabric-spanning-entire-ai-data-centers/ ; "One-chip-like datacenter design enabled by CXL-based scale-up fabrics", *Nature Reviews Electrical Engineering* (2026). https://www.nature.com/articles/s44287-026-00315-5
- **ocp2026** — "OCP 2026: Agentic AI Pushes the Data Center from Chip to Grid". https://www.datacenterfrontier.com/hyperscale/article/55411534/ocp-2026-agentic-ai-pushes-the-data-center-from-chip-to-grid
- **fhe2026** — Niobium, "The Fog": encrypted cloud for FHE workloads, private beta April 2026, running on the mistic Core FPGA accelerator; Niobium's FHE ASIC is in development. https://www.darkreading.com/endpoint-security/niobium-introduces-the-fog ; https://siliconangle.com/?p=737132

## Economics and energy

- **capex2026** — Yield Theory, "Hyperscaler Capex 2026: $720B to $740B Guidance by Company" (updated 5 November 2026). https://www.yieldtheory.app/research/hyperscaler-ai-capex-tracker-2026
- **gartner2026** — Gartner, "Gartner Forecasts Worldwide IT Spending to Grow 14.2% in 2026, Totaling $6.37 Trillion", 27 July 2026. https://www.gartner.com/en/newsroom/press-releases/2026-07-27-gartner-forecasts-worldwide-it-spending-to-grow-14-point-2-percent-in-2026-totaling-6-point-37-trillion ; the Gartner page refuses automated fetching, the release is checked through TelecomTV: https://www.telecomtv.com/content/digital-platforms-services/gartner-forecasts-worldwide-it-spending-to-grow-14-2-in-2026-totalling-6-37tn-55964/
- **ai-debt** — "Morgan Stanley estimates global AI debt issuance to double to $570 billion in 2026" (Reuters via newkerala, 10 June 2026): "global debt issuance for AI companies to top $500 billion in 2026". https://www.newkerala.com/news/a/morgan-stanley-estimates-global-ai-debt-issuance-double-248.htm ; background: Forbes, "The Half-Trillion-Dollar Silicon Gamble", 2026 (not reachable for automated checking).
- **ap2-fido** — "FIDO Alliance to start work on interoperable standards for agentic commerce", Biometric Update, April 2026: work drawing on initial contributions from Google (AP2) and Mastercard. https://www.biometricupdate.com/202604/fido-alliance-to-start-work-on-interoperable-standards-for-agentic-commerce
- **boe-fsr-2026** — Bank of England, Financial Stability Report, July 2026, section 2.3: "chips have short and uncertain lifecycles … debt maturities aligned with the asset life of AI chips would imply shorter tenors than are typically preferred by most credit investors". https://www.bankofengland.co.uk/-/media/boe/files/financial-stability-report/2026/financial-stability-report-july-2026.pdf
- **woodmac-transformers** — Wood Mackenzie, "Supply shortages and an inflexible market give rise to high power transformer lead times": lead times from about 50 weeks in 2021 to 120 weeks on average in 2024. https://www.woodmac.com/news/opinion/supply-shortages-and-an-inflexible-market-give-rise-to-high-power-transformer-lead-times/
- **scripps-o2** — Scripps O2 Program: "The observed downward trend amounts to 19 'per meg' per year", mostly from fossil-fuel burning. https://scrippso2.ucsd.edu
- **primavera2021** — B. A. Primavera, J. M. Shainline, "Considerations for neuromorphic supercomputing in semiconducting and superconducting optoelectronic hardware", arXiv:2106.14803: Carnot limit 74 W/W at 4 K from 300 K; "State-of-the-art systems have reached specific powers below 400 W/W". https://arxiv.org/abs/2106.14803
- **stockholm-data-parks** — Stockholm Data Parks: heat recovery from data centres paid for by the district heating network. https://stockholmdataparks.com/
- **spolsky** — J. Spolsky, "Strategy Letter V" (commoditize your complements), 2002. https://www.joelonsoftware.com/2002/06/12/strategy-letter-v/
- **rfc6962** — B. Laurie, A. Langley, E. Kasper, "Certificate Transparency", RFC 6962, 2013 (the version deployed logs implement). https://www.rfc-editor.org/rfc/rfc6962
- **epoch-prices** — Epoch AI, LLM inference price trends. https://epoch.ai/blog/llm-inference-price-trends
- **compute-futures** — FIA, "What are compute futures". https://www.fia.org/fia/articles/explainer-what-are-compute-futures-and-how-do-they-work
- **x402** — Linux Foundation, "Linux Foundation is Launching the x402 Foundation and Welcoming the Contribution of the x402 Protocol", 2 April 2026. https://www.linuxfoundation.org/press/linux-foundation-is-launching-the-x402-foundation-and-welcoming-the-contribution-of-the-x402-protocol
- **outcome-pricing** — EQT Ventures on outcome-based pricing. https://stories.eqtventures.com/articles/monetising-the-agentic-workforce-is-outcome-based-pricing-the-answer
- **data-act** — Regulation (EU) 2023/2854 (Data Act), Article 29: "From 12 January 2027, providers of data processing services shall not impose any switching charges on the customer for the switching process." https://eur-lex.europa.eu/legal-content/EN/TXT/HTML/?uri=CELEX:32023R2854
- **power2026** — Grid interconnection and data centre power. https://www.softwareseni.com/the-broken-power-grid-infrastructure-bottlenecks-driving-the-nuclear-push-for-ai-data-centres/
- **aslan2018** — J. Aslan, K. Mayers, J. Koomey, C. France, "Electricity Intensity of Internet Data Transmission: Untangling the Estimates", *Journal of Industrial Ecology* 22(4), 2018, doi:10.1111/jiec.12630: 0.06 kWh/GB in 2015, halving about every two years. We extrapolate to 2026 (0.06 / 2^5.5 kWh/GB); the extrapolation is ours.
- **ipcc2014** — IPCC AR5 WGIII Annex III, Table A.III.2, life-cycle emission medians (gCO2e/kWh): coal 820, gas 490, biomass 230, solar PV utility 48, geothermal 38, hydro 24, nuclear 12, wind 11.5/12. As reproduced at https://lowcarbonpower.org/de/blog/emissions ; NREL harmonisation for comparison: https://www.nrel.gov/docs/fy21osti/80580.pdf

## Foresight

- **iftf2036** — Institute for the Future, Ten-Year Forecast 2036 "What We Choose Next" (October 2026). https://www.iftf.org/projects/iftf-ten-year-forecast-2036-tyf/
- **iftf2025** — IFTF Ten-Year Forecast 2025–2035 "Strategy in the Age of Chaos". https://www.iftf.org/events/iftf-ten-year-forecast-2025-2035-sanctuaries-of-social-imagination/ ; perspectives: https://www.iftf.org/projects/iftf-ten-year-forecast-urgent-futures-perspectives/
- **ftsg2026** — Future Today Strategy Group, Convergence Outlook 2026 (Amy Webb, SXSW 2026), as reported by Meio & Mensagem and FTSG's own summaries; the full report is behind a form. https://sxsw.md/news/2026-03-16-amy-webb-end-of-trend-reports ; https://fortune.com/brandstudio/future-today-strategy-group/the-5-convergences-that-will-redefine-business
- **abi-physical** — ABI Research, "Physical AI at Scale: Why Robotics Needs a New Data Infrastructure". https://www.abiresearch.com/blog/physical-ai-at-scale-why-robotics-needs-a-new-data-infrastructure

## Data and benchmarks

- **gset-best** — Gset best-known MaxCut values: G1 = 11624, G2 = 11620, G22 = 13359, confirmed in two independent sources (e.g. arXiv:2606.03917 and the Tabu-search comparison table of Ma and Hao 2017 as cited). Instances: https://web.stanford.edu/~yyye/yyye/Gset/
- **optdigits** — E. Alpaydin, C. Kaynak, "Optical Recognition of Handwritten Digits", UCI Machine Learning Repository (CC BY 4.0). https://archive.ics.uci.edu/ml/machine-learning-databases/optdigits/
- **energy-charts** — Fraunhofer ISE, Energy-Charts API (public net generation; day-ahead prices CC BY 4.0 from Bundesnetzagentur | SMARD.de). https://api.energy-charts.info/
- **uk-ci** — National Energy System Operator, Carbon Intensity API (forecast and actual, CC BY 4.0). https://carbonintensity.org.uk/
- **amd-9454p** — AMD EPYC 9454P: 48 cores, 290 W default TDP, configurable 240–300 W (reseller specification sheets; AMD lists the same). https://www.scan.co.uk/products/amd-epyc-9454p-s-sp5-5nm-zen-4-48-core-96-thread-275ghz-38ghz-turbo-256mb-290w-cpu-oem
