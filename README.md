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
4. Collect metrics with: `./dist/perfman collect -n=<dir-name> -l=<time-period> -m=<metric-group>`. `dir-name`
corresponds to the name of the subdirectory that will be created under `output/`, where each file in this subdirectory
will be a CSV file with data pertaining to the metrics associated with the specified metric group. `time-period` is
the amount of time you would like to look back from the current time for the metric collection, and must be less than or
equal to `n` in step 3. `metric-group` is the metric group to use. The metrics corresponding to a
metric group can be found in `metrics/metric_groups.go`

## Dashboard

The `dashboard` command imports a Grafana dashboard from a template and optionally creates a shareable snapshot. The repo ships a **pipeline** template; use `--template-path` to load any other dashboard JSON (e.g. MonoVertex or custom) from a file.

**Prerequisites:** Grafana and Prometheus must be reachable. If you use `perfman setup`, install Grafana with the `-g` flag. Port-forward Grafana and Prometheus (e.g. `./dist/perfman portforward -p -g`) so perfman can talk to them.

**Usage:**

```bash
# Import a dashboard (live dashboard in Grafana; prints the dashboard URL)
./dist/perfman dashboard [--template pipeline | --template-path <path>]

# Create a snapshot (frozen, shareable link; prints the snapshot URL)
./dist/perfman dashboard --snapshot [--template pipeline | --template-path <path>]
```

**Options:**

| Option            | Description                                                                 |
|-------------------|-----------------------------------------------------------------------------|
| `-t pipeline`     | Use the built-in pipeline metrics dashboard (default).                      |
| `--template-path` | Path to a dashboard JSON file (overrides `--template`). Use for MonoVertex or custom templates kept outside this repo. |
| `--snapshot`      | Create a snapshot and print the snapshot URL instead of the live dashboard URL. |

**Examples:**

- Import the pipeline dashboard:  
  `./dist/perfman dashboard` or `./dist/perfman dashboard -t pipeline`

- Import a dashboard from your own file (e.g. MonoVertex):  
  `./dist/perfman dashboard --template-path /path/to/dashboard-monovertex-template.json`

- Create a shareable snapshot of the pipeline dashboard:  
  `./dist/perfman dashboard -t pipeline --snapshot`

- Create a snapshot from a custom template:  
  `./dist/perfman dashboard --template-path ./my-dashboard.json --snapshot`

**Snapshot limitation:** Snapshots created via the CLI may show **empty panels** (no inbound/outbound curves). Grafana’s snapshot API expects the dashboard payload to include pre-run panel data; we only send the dashboard definition (queries), not query results. For a snapshot that includes real data, open the dashboard in Grafana, let it load, then use **Share → Snapshot** in the UI.

**Sharing snapshots externally:** The snapshot URL uses your Grafana base URL (e.g. `http://localhost:3000/...`), so it only works on your machine. To share with others:
- Expose Grafana at a **public URL** (e.g. [ngrok](https://ngrok.com/) for local: `ngrok http 3000`, or deploy Grafana in a cluster with an ingress).
- Configure Grafana’s **root_url** to that public URL so generated snapshot links point there.
- Then the snapshot link from the CLI (or from the UI) will be viewable by anyone with the link.