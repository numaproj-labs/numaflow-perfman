# Numaflow Perfman

[![Go Report Card](https://goreportcard.com/badge/github.com/numaproj-labs/numaflow-perfman)](https://goreportcard.com/report/github.com/numaproj-labs/numaflow-perfman)

Perfman assumes your system has the following already installed:
- Local kubernetes cluster with at least a single node

# POC Demo Steps

After creating the perfman executable with `make build`:

1. Set up the environment required for perfman: `./perfman setup`
2. Port forward Prometheus: `./perfman portforward -p`. Port forward Grafana in separate tab: `./perfman portforward -g`
3. Start a pipeline: `./perfman pipeline`. Allow the pipeline to gather metrics for at least 5 minutes
4. Run the report script: `./report-poc.sh`. This will output prometheus data as well as a report under the `test/` folder
5. If you would like to visualize your metrics as continuous time series data. First login
to Grafana at `localhost:3000`, then run `./perfman report` and follow the URL
