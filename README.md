# Numaflow Perfman

[![Go Report Card](https://goreportcard.com/badge/github.com/numaproj-labs/numaflow-perfman)](https://goreportcard.com/report/github.com/numaproj-labs/numaflow-perfman)

## Running Perfman 

Perfman assumes your system already has a local kubernetes cluster with at least a single node configured. Perfman can be
run in two different ways. 

With an executable:
1. Clone the repo and make it the active working directory: `git clone git@github.com:numaproj-labs/numaflow-perfman.git && cd numaflow-perfman`
2. Run `make build`. This will produce an executable: `dist/perfman`
3. Use perfman with desired commands: `./dist/perfman help`

Note: if you use your current terminal session to port-forward, since this is a blocking operation, open a new 
terminal tab to run new commands.

With Docker:
1. Pull the stable image from registry: `docker pull quay.io/numaio/numaproj-labs/perfman:stable`
2. Run the container with `mkdir -p output && docker run -it --network host -v ~/.kube/config:/perfmanuser/.kube/config:ro 
-v ./output:/home/perfman/output quay.io/numaio/numaproj-labs/perfman:stable` (if you have the repo cloned, this can be
accomplished with `make run`). This assumes that the local kubernetes configuration file for your cluster is located at
`$HOME/.kube/config`
3. The above command will spawn a shell inside the container, and perfman can be used with the desired commands:
`perfman help`

Note: As above, port-forwarding is a blocking operation, and with the Docker option, open a new terminal tab and:
- Identify the name/ID of the currently running perfman container with `docker ps`
- Run `docker exec -it <name/ID> /bin/ash`. This will provide a new terminal session inside the same container, 
where you can run additional commands

## Collecting Metrics

The following steps can be followed in order to collect metrics for the base perfman pipeline. Note that the metrics
are outputted as CSV files under the `output/` directory. The following steps assume that you are running the perfman
executable, but the same steps can easily be modified for the Docker case.

1. Run `./dist/perfman setup`, this will deploy the Prometheus Operator and create the service monitors for scraping 
pipeline and ISB metrics. Pass the `-n` and/or `-j` flags to the command if you do not have Numaflow and/or 
NATS Jetsream ISB running on the cluster
2. In a separate terminal, port-forward the Prometheus service to `localhost:9090` with `./dist/perfman portforward -p`
3. Deploy the perfman base pipeline: `./dist/perfman pipeline`. Allow `n` minutes to pass in order for metrics to be
collected for your desired length of time
4. Collect metrics with: `./dist/perfman -n=<dir-name> -l=<time-period> -m=<metric-group>`. `dir-name`
corresponds to the name of the subdirectory that will be created under `output/`, where each file in this subdirectory
will be a CSV file with data pertaining to the metrics associated with the specified metric group. `time-period` is
the amount of time you would like to look back from the current time for the metric collection, and must be less than or
equal to `n` in step 4. `metric-group` is the metric group to use. The metrics corresponding to a
metric group can be found in `metrics/metric_groups.go`
