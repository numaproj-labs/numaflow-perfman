package util

import (
	"context"
	"errors"
	"fmt"
	logger "log"
	"os"

	"go.uber.org/zap"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/storage/driver"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type ChartRelease struct {
	ChartName   string
	ReleaseName string
	RepoUrl     string
	Namespace   string
	Values      map[string]interface{}
}

var settings = cli.New()
var actionConfig = new(action.Configuration)

func getChart(chartPathOption action.ChartPathOptions, chartName string, settings *cli.EnvSettings) (*chart.Chart, error) {
	chartPath, err := chartPathOption.LocateChart(chartName, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate %s: %w", chartName, err)
	}

	c, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load %s: %w", chartName, err)
	}

	return c, nil
}

func createNamespace(kubeClient *kubernetes.Clientset, nso *v1.Namespace) error {
	_, err := kubeClient.CoreV1().Namespaces().Create(context.TODO(), nso, metav1.CreateOptions{})
	if kerrors.IsAlreadyExists(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	return nil
}

func (cr *ChartRelease) InstallOrUpgradeRelease(kubeClient *kubernetes.Clientset, log *zap.Logger) error {
	namespaceObject := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Namespace,
		},
	}

	if err := createNamespace(kubeClient, namespaceObject); err != nil {
		return err
	}

	if err := actionConfig.Init(settings.RESTClientGetter(), cr.Namespace, os.Getenv("HELM_DRIVER"), logger.Printf); err != nil {
		return fmt.Errorf("failed to initialize actionConfig: %w", err)
	}

	chartPathOptions := action.ChartPathOptions{
		RepoURL: cr.RepoUrl,
	}

	c, err := getChart(chartPathOptions, cr.ChartName, settings)
	if err != nil {
		return fmt.Errorf("failed to get chart: %w", err)
	}

	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	if _, err := histClient.Run(cr.ReleaseName); errors.Is(err, driver.ErrReleaseNotFound) {
		clientInstall := action.NewInstall(actionConfig)
		clientInstall.ReleaseName = cr.ReleaseName
		clientInstall.Namespace = cr.Namespace
		clientInstall.ChartPathOptions = chartPathOptions

		rel, err := clientInstall.Run(c, cr.Values)
		if err != nil {
			return fmt.Errorf("failed to install %s: %w", cr.RepoUrl, err)
		}

		log.Info("installed chart successfully", zap.String("release-name", rel.Name), zap.String("release-namespace", rel.Namespace))
	} else {
		clientUpgrade := action.NewUpgrade(actionConfig)
		clientUpgrade.Namespace = cr.Namespace
		clientUpgrade.ChartPathOptions = chartPathOptions

		rel, err := clientUpgrade.Run(cr.ReleaseName, c, cr.Values)
		if err != nil {
			return fmt.Errorf("failed to upgrade %s: %w", cr.RepoUrl, err)
		}

		log.Info("updated chart successfully", zap.String("release-name", rel.Name), zap.String("release-namespace", rel.Namespace))
	}

	return nil
}

func UninstallRelease(releaseName string, namespace string, log *zap.Logger) error {
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), logger.Printf); err != nil {
		return fmt.Errorf("failed to initialize actionConfig: %w", err)
	}

	// Check if release exists
	getStatus := action.NewGet(actionConfig)
	if _, err := getStatus.Run(releaseName); err != nil {
		log.Warn("release does not exist, it might already be un-installed", zap.String("release-name", releaseName))
		return nil
	}

	uninstall := action.NewUninstall(actionConfig)
	_, err := uninstall.Run(releaseName)
	if err != nil {
		return fmt.Errorf("failed to uninstall release: %s: %w", releaseName, err)
	}

	return nil
}
