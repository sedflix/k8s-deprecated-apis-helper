package main

import (
	"bytes"
	"fmt"
	plutoversionsfile "github.com/fairwindsops/pluto/v5"
	"github.com/fairwindsops/pluto/v5/pkg/api"
	"github.com/fairwindsops/pluto/v5/pkg/finder"
	"gopkg.in/yaml.v3"
	"log"
	"os"
	"os/exec"
)

var (
	apiInstance *api.Instance
)

// Cluster as defined in argocd-apps repo
type Cluster struct {
	Autosync          bool     `yaml:"autosync,omitempty"`
	Chart             string   `yaml:"chart,omitempty"`
	ChartVersion      string   `yaml:"chartVersion,omitempty"`
	GitRepo           string   `yaml:"gitRepo,omitempty"`
	HelmRepo          string   `yaml:"helmRepo,omitempty"`
	IgnoreDifferences []string `yaml:"ignoreDifferences,omitempty"`
	Name              string   `yaml:"name,omitempty"`
	SyncOptions       []string `yaml:"syncOptions,omitempty"`
	ValuesFiles       []string `yaml:"valuesFiles,omitempty"`
}

type Zone struct {
	Alias       string `yaml:"alias,omitempty"`
	Clusters    map[string]Cluster
	Description string `yaml:"description,omitempty"`
	Endpoint    string `yaml:"endpoint,omitempty"`
}

type ArgoCdApps struct {
	Zones map[string]Zone
}

// parseArgocdAppsFile reads and parse an argocd-apps file to return a struct for the same
func parseArgocdAppsFile(filepath string) ArgoCdApps {

	// read the file
	data, err := os.ReadFile(filepath)
	if err != nil {
		log.Fatal(err)
	}

	// make the struct
	argocdApps := ArgoCdApps{}

	// parse the read file into the struct
	err = yaml.Unmarshal(data, &argocdApps)
	if err != nil {
		log.Fatal(err)
	}

	return argocdApps
}

// initialiseApiInstance initialise pluto being stored at apiInstance
func initialiseApiInstance() {
	// get information of all the deprecated content
	var versionsFile []byte = plutoversionsfile.Content()
	defaultDeprecatedVersions, _, _ := api.GetDefaultVersionList(versionsFile)

	//  finally initialise it apiInstance
	apiInstance = &api.Instance{
		TargetVersions:     map[string]string{"k8s": "v1.23.0"},
		OutputFormat:       "wide",
		CustomColumns:      nil,
		IgnoreDeprecations: true,
		IgnoreRemovals:     false,
		OnlyShowRemoved:    true,
		DeprecatedVersions: defaultDeprecatedVersions,
		Components:         []string{"k8s"},
	}
	return
}

// templateChart  templates the given `chart` with the given `valuesFiles` and `version`
// if version not specified the latest version will be templated
// if valuesFiles not present, only values.yaml if present will be used the values file
// helm template [chart]  [-f valuesFiles[0] ...] [--version version]
func templateChart(chart string, valuesFiles []string, version string) ([]byte, error) {

	// helmArgs is used to store all arguments that we need to pass to helm command
	var helmArgs []string
	helmArgs = append(helmArgs, "template")
	helmArgs = append(helmArgs, chart)

	// if values file is present add [-f valuesFiles[0] ...]
	for _, file := range valuesFiles {
		helmArgs = append(helmArgs, "-f", file)
	}

	// if version is set use that particular version.
	// not setting version with template the latest chart
	if len(version) > 0 {
		helmArgs = append(helmArgs, "--version", version)
	}

	// prepare the command
	helmTemplateCmd := exec.Command("helm", helmArgs...)

	// prepare the output buffer for the command
	var helmTemplateOutput bytes.Buffer
	helmTemplateCmd.Stdout = &helmTemplateOutput

	// run the command
	err := helmTemplateCmd.Run()
	if err != nil {
		log.Fatal(err)
	}

	// return the output
	return helmTemplateOutput.Bytes(), nil
}

func main() {

	parseArgocdAppsFile("./tests/argocd-apps-test.yaml")

	initialiseApiInstance()
	// dir is finder dirlication
	dir := finder.Dir{
		Instance: apiInstance,
	}

	data, _ := templateChart("chartrepo/apollo", []string{}, "")

	dir.Instance.Outputs, _ = dir.Instance.IsVersioned(data)
	dir.Instance.FilterOutput()
	fmt.Println(dir.Instance.Outputs)
}
