# Grafana dashboards (f12-03 / f12-04)

Dashboard definitions live here as Grafana dashboard JSON so the repository is
the single source of truth (provisioning, not hand-editing in the UI). They are
wired through the `lattice.grafana.dashboardProviders` option and loaded by the
Grafana file provider under the "Lattice" folder.

This directory intentionally starts empty; the concrete fleet of dashboards
("LLM Gateway", "Gateway runtime", "Loki / расследование") is delivered by
f12-04. Until then Grafana runs with the Prometheus and Loki datasources
provisioned but no dashboard files.
