package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/numaproj-labs/numaflow-perfman/util"
)

type DashboardResponse struct {
	ID    int    `json:"id"`
	UID   string `json:"uid"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func ReadJSONFile(filePath string) ([]byte, error) {
	return os.ReadFile(filePath)
}

func CreateDashboard(grafanaURL, auth string, dashboardData []byte) (DashboardResponse, error) {
	var response DashboardResponse
	createURL := grafanaURL + "/api/dashboards/db"
	req, err := http.NewRequest("POST", createURL, bytes.NewBuffer(dashboardData))
	if err != nil {
		return response, err
	}
	req.Header.Add("Authorization", "Basic "+auth)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return response, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return response, err
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return response, fmt.Errorf("error parsing JSON response: %v", err)
	}

	return response, nil
}

func FetchDashboard(grafanaURL, auth, dashboardID string) ([]byte, error) {
	// dashboardURL := fmt.Sprintf("%s/api/dashboards/db/%s", grafanaURL, dashboardName)
	dashboardURL := fmt.Sprintf("%s/api/dashboards/uid/%s", grafanaURL, dashboardID)

	req, _ := http.NewRequest("GET", dashboardURL, nil)
	req.Header.Add("Authorization", "Basic "+auth)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func CreateSnapshot(grafanaURL, auth string, dashboardData []byte) (string, error) {
	snapshotURL := grafanaURL + "/api/snapshots"
	req, _ := http.NewRequest("POST", snapshotURL, bytes.NewBuffer(dashboardData))
	req.Header.Add("Authorization", "Basic "+auth)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("error parsing JSON response: %v", err)
	}
	if result.URL == "" {
		return "", fmt.Errorf("snapshot URL not found in response")
	}
	return result.URL, nil
}

// CreateGrafanaDataSource assumes there is a prometheus server running on http://localhost:9090
// and creates a new Grafana data source called Numaflow-PerfMan-Prometheus connecting to prometheus.
// It returns the uid of the created data source.
func CreateGrafanaDataSource(grafanaURL, auth string) (string, error) {

	dataSource := map[string]interface{}{
		"name":      "Numaflow-PerfMan-Prometheus",
		"type":      "prometheus",
		"url":       fmt.Sprintf("http://%s:9090", util.PrometheusPFServiceName),
		"access":    "proxy",
		"isDefault": false,
	}

	data, err := json.Marshal(dataSource)
	if err != nil {
		return "", err
	}

	dataSourceURL := grafanaURL + "/api/datasources"
	req, _ := http.NewRequest("POST", dataSourceURL, bytes.NewBuffer(data))
	req.Header.Add("Authorization", "Basic "+auth)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("failed to fetch data sources: %s", string(body))
	}

	result := struct {
		Datasource struct {
			UID string `json:"uid"`
		} `json:"datasource"`
		Message string `json:"message"`
	}{}

	err = json.Unmarshal(body, &result)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal JSON response: %s", err)
	}
	return result.Datasource.UID, nil
}

// GrafanaDatasource for parsing datasource details
type GrafanaDatasource struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

// FetchGrafanaDataSourceUID fetches the UID of an existing data source, given the name.
func FetchGrafanaDataSourceUID(grafanaURL, auth string) (string, error) {
	dataSourceURL := grafanaURL + "/api/datasources"
	req, _ := http.NewRequest("GET", dataSourceURL, nil)
	req.Header.Add("Authorization", "Basic "+auth)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("failed to create data source: %s", string(body))
	}

	var dataSources []GrafanaDatasource
	err = json.Unmarshal(body, &dataSources)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal JSON response: %s", err)
	}

	for _, ds := range dataSources {
		if ds.Name == "Numaflow-PerfMan-Prometheus" {
			return ds.UID, nil
		}
	}

	return "", fmt.Errorf("data source not found: %s", "Numaflow-PerfMan-Prometheus")
}
