package communitymodules

import (
	"encoding/json"
	"fmt"
	"golang.org/x/mod/semver"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/kyma-project/cli.v3/internal/clierror"
	"github.com/kyma-project/cli.v3/internal/cmdcommon"
	"github.com/kyma-project/cli.v3/internal/kyma"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const URL = "https://raw.githubusercontent.com/kyma-project/community-modules/main/model.json"

type row struct {
	Name          string
	Repository    string
	LatestVersion string
	Version       string
	Managed       string
}

type moduleMap map[string]row

// ModulesCatalog returns a map of all available modules and their repositories, if the map is nil it will create a new one
func ModulesCatalog() (moduleMap, clierror.Error) {
	return modulesCatalog(URL)
}

func modulesCatalog(url string) (moduleMap, clierror.Error) {
	modules, err := getCommunityModules(url)
	if err != nil {
		return nil, err
	}

	catalog := make(moduleMap)
	for _, rec := range modules {
		latestVersion := getLatestVersion(rec.Versions)
		catalog[rec.Name] = row{
			Name:          rec.Name,
			Repository:    chooseRepository(rec, latestVersion),
			LatestVersion: latestVersion.Version,
		}
	}
	return catalog, nil
}

// chooseRepository returns the repository of the module for specific version if it is available, otherwise it returns the repository of the module.
// Sometimes one of those values don't exist so this function makes sure that we provide the user with the most information possible.
func chooseRepository(module Module, version Version) string {
	if version.Repository != "" {
		return version.Repository
	}
	if module.Repository != "" {
		return module.Repository
	}
	return "Unknown"
}
func getLatestVersion(versions []Version) Version {
	return slices.MaxFunc(versions, func(a, b Version) int {
		cmpA := a.Version
		if !semver.IsValid(cmpA) {
			cmpA = fmt.Sprintf("v%s", cmpA)
		}
		cmpB := b.Version
		if !semver.IsValid(cmpB) {
			cmpB = fmt.Sprintf("v%s", cmpB)
		}
		return semver.Compare(cmpA, cmpB)
	})
}

// getCommunityModules returns a list of all available modules from the community-modules repository
func getCommunityModules(url string) (Modules, clierror.Error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, clierror.Wrap(err, clierror.New("while getting modules list from github"))
	}
	defer resp.Body.Close()

	var modules Modules
	modules, respErr := decodeCommunityModulesResponse(err, resp, modules)
	if respErr != nil {
		return nil, clierror.WrapE(respErr, clierror.New("while handling response"))
	}
	return modules, nil
}

// decodeCommunityModulesResponse reads the response body and unmarshals it into the template
func decodeCommunityModulesResponse(err error, resp *http.Response, modules Modules) (Modules, clierror.Error) {
	if resp.StatusCode != 200 {
		errMsg := fmt.Sprintf("error response: %s", resp.Status)
		return nil, clierror.Wrap(err, clierror.New(errMsg))
	}

	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierror.Wrap(err, clierror.New("while reading http response"))
	}

	err = json.Unmarshal(bodyText, &modules)
	if err != nil {
		return nil, clierror.Wrap(err, clierror.New("while unmarshalling"))
	}
	return modules, nil
}

// ManagedModules returns a map of all managed modules from the cluster
func ManagedModules(client cmdcommon.KubeClientConfig, cfg cmdcommon.KymaConfig) (moduleMap, clierror.Error) {
	moduleNames, err := getManagedList(client, cfg)
	if err != nil {
		return nil, clierror.WrapE(err, clierror.New("while getting managed modules"))
	}

	managed := make(moduleMap)
	for _, name := range moduleNames {
		managed[name] = row{
			Name:    name,
			Managed: "True",
		}
	}
	return managed, nil
}

// getManagedList gets a list of all managed modules from the Kyma CR
func getManagedList(client cmdcommon.KubeClientConfig, cfg cmdcommon.KymaConfig) ([]string, clierror.Error) {
	resp, err := kyma.GetDefaultKyma(cfg.Ctx, client.KubeClient)
	if err != nil && !errors.IsNotFound(err) {
		return nil, clierror.Wrap(err, clierror.New("while getting Kyma CR"))
	}
	if errors.IsNotFound(err) {
		return nil, nil
	}

	moduleNames, err := decodeKymaCRResponse(resp)
	if err != nil {
		return nil, clierror.Wrap(err, clierror.New("while getting module names from CR"))
	}
	return moduleNames, nil
}

// decodeKymaCRResponse interprets the response and returns a list of managed modules names
func decodeKymaCRResponse(unstruct *unstructured.Unstructured) ([]string, error) {
	var moduleNames []string
	managedFields := unstruct.GetManagedFields()
	for _, field := range managedFields {
		var data map[string]interface{}
		err := json.Unmarshal(field.FieldsV1.Raw, &data)
		if err != nil {
			return nil, err
		}

		spec, ok := data["f:spec"].(map[string]interface{})
		if !ok {
			continue
		}

		modules, ok := spec["f:modules"].(map[string]interface{})
		if !ok {
			continue
		}

		moduleNames = append(moduleNames, extractNames(modules)...)
	}
	return moduleNames, nil
}

func extractNames(modules map[string]interface{}) []string {
	var moduleNames []string
	for key := range modules {
		if strings.Contains(key, "name") {
			name := strings.TrimPrefix(key, "k:{\"name\":\"")
			name = strings.Trim(name, "\"}")
			moduleNames = append(moduleNames, name)
		}
	}
	return moduleNames
}

// InstalledModules returns a map of all installed modules from the cluster, regardless whether they are managed or not
func InstalledModules(client cmdcommon.KubeClientConfig, cfg cmdcommon.KymaConfig) (moduleMap, clierror.Error) {
	return installedModules(URL, client, cfg)
}

func installedModules(url string, client cmdcommon.KubeClientConfig, cfg cmdcommon.KymaConfig) (moduleMap, clierror.Error) {
	modules, err := getCommunityModules(url)
	if err != nil {
		return nil, clierror.WrapE(err, clierror.New("while getting installed modules"))
	}

	installed, err := getInstalledModules(modules, client, cfg)
	if err != nil {
		return nil, err
	}

	return installed, nil
}

func getInstalledModules(modules Modules, client cmdcommon.KubeClientConfig, cfg cmdcommon.KymaConfig) (moduleMap, clierror.Error) {
	installed := make(moduleMap)
	for _, module := range modules {
		latestVersion := getLatestVersion(module.Versions)
		managerName := getManagerName(latestVersion)
		deployment, err := client.KubeClient.Static().AppsV1().Deployments("kyma-system").
			Get(cfg.Ctx, managerName, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			msg := "while getting the " + managerName + " deployment"
			return nil, clierror.Wrap(err, clierror.New(msg))
		}
		if errors.IsNotFound(err) {
			continue
		}

		installedVersion := getInstalledVersion(deployment)
		moduleVersion := latestVersion.Version
		installed[module.Name] = row{
			Name:    module.Name,
			Version: calculateVersion(moduleVersion, installedVersion),
		}
	}
	return installed, nil
}

func getInstalledVersion(deployment *v1.Deployment) string {
	deploymentImage := strings.Split(deployment.Spec.Template.Spec.Containers[0].Image, "/")
	nameAndTag := strings.Split(deploymentImage[len(deploymentImage)-1], ":")
	return nameAndTag[len(nameAndTag)-1]
}

func getManagerName(version Version) string {
	managerPath := strings.Split(version.ManagerPath, "/")
	return managerPath[len(managerPath)-1]
}

func calculateVersion(moduleVersion string, installedVersion string) string {
	if moduleVersion == installedVersion {
		return installedVersion
	}
	return "outdated moduleVersion, latest is " + moduleVersion
}