package main

import (
	"bytes"
	plutoversionsfile "github.com/fairwindsops/pluto/v5"
	"github.com/fairwindsops/pluto/v5/pkg/api"
	"github.com/fairwindsops/pluto/v5/pkg/finder"
	"gopkg.in/yaml.v3"
	"log"
	"os"
	"os/exec"
	"strings"
)

var (
	apiInstance *api.Instance
)

const (
	workDirPath string = "/tmp/workdir"
	FAILED      int    = 0
	PASS        int    = 1
	UNKNOWN     int    = 2
)

const ()

var state2string = map[int]string{
	0: "Failed",
	1: "Passed",
	2: "Unknown",
}

// Cluster as defined in argocd-apps repo
type Cluster struct {
	Autosync          bool        `yaml:"autosync,omitempty"`
	Chart             string      `yaml:"chart,omitempty"`
	ChartVersion      string      `yaml:"chartVersion,omitempty"`
	GitRepo           string      `yaml:"gitRepo,omitempty"`
	HelmRepo          string      `yaml:"helmRepo,omitempty"`
	IgnoreDifferences interface{} `yaml:"ignoreDifferences,omitempty"`
	Name              string      `yaml:"name,omitempty"`
	SyncOptions       []string    `yaml:"syncOptions,omitempty"`
	ValuesFiles       []string    `yaml:"valuesFiles,omitempty"`
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
		log.Printf("Error Reading %s due to %s", filepath, err)
	}

	// make the struct
	argocdApps := ArgoCdApps{}

	// parse the read file into the struct
	err = yaml.Unmarshal(data, &argocdApps)
	if err != nil {
		log.Printf("Error Unmarshalling due to %s", err)
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

// fetchChart will do a `helm fetch chart`  --version <version> --untar -d workDirPath
func fetchChart(repo string, chart string, version string) (string, error) {

	// helmFetchArgs is used to store all arguments that we need to pass to helm command for
	var helmFetchArgs []string
	helmFetchArgs = append(helmFetchArgs, "fetch")
	helmFetchArgs = append(helmFetchArgs, repo+"/"+chart)

	// if version is set use that particular version.
	// not setting version with template the latest chart
	if len(version) > 0 && !strings.Contains(version, "*") && !strings.Contains(version, "~") && !strings.Contains(version, "^") && !strings.Contains(version, "-") {
		helmFetchArgs = append(helmFetchArgs, "--version", version)
	}

	helmFetchArgs = append(helmFetchArgs, "--untar")
	helmFetchArgs = append(helmFetchArgs, "-d", workDirPath)

	// prepare the command
	helmFetchCmd := exec.Command("helm", helmFetchArgs...)

	// prepare the output buffer for the command
	var helmFetchOutput bytes.Buffer
	helmFetchCmd.Stdout = &helmFetchOutput

	// prepare the output buffer for the command for storing the error output
	var helmFetchOutputErr bytes.Buffer
	helmFetchCmd.Stderr = &helmFetchOutputErr

	// remove the path before running
	err := os.RemoveAll(workDirPath + "/" + chart)
	if err != nil {
		log.Printf("Unable to remove %s due to %s", workDirPath+"/"+chart, err)
		return "", err
	}

	// run the command
	log.Println(helmFetchCmd.String())
	err = helmFetchCmd.Run()

	if err != nil {
		log.Printf("Helm Fetch %s  failed due to %s", helmFetchCmd.String(), helmFetchOutputErr.String())
		return "", err
	}

	return workDirPath + "/" + chart, nil
}

// templateChart  templates the fetch given `chart` from `repo` with the given `valuesFiles` and `version`
// if version not specified the latest version will be templated
// if valuesFiles not present, only values.yaml if present will be used the values file
// helm template [chart]  [-f valuesFiles[0] ...] [--version version]
func templateChart(repo string, chart string, valuesFiles []string, version string) ([]byte, error) {

	path, err := fetchChart(repo, chart, version)
	if err != nil {
		return nil, err
	}

	// helmTemplateArgs is used to store all arguments that we need to pass to helm command
	var helmTemplateArgs []string
	helmTemplateArgs = append(helmTemplateArgs, "template")
	helmTemplateArgs = append(helmTemplateArgs, path)

	// if values file is present add [-f valuesFiles[0] ...]
	for _, file := range valuesFiles {
		helmTemplateArgs = append(helmTemplateArgs, "-f", path+"/"+file)
	}

	// prepare the command
	helmTemplateCmd := exec.Command("helm", helmTemplateArgs...)

	// prepare the output buffer for the command. this buffer will store the templated chart
	var helmTemplateOutput bytes.Buffer
	helmTemplateCmd.Stdout = &helmTemplateOutput

	// prepare the output buffer for the command error message. used for printing the templating error reason.
	var helmTemplateOutputErr bytes.Buffer
	helmTemplateCmd.Stderr = &helmTemplateOutputErr

	// run the command
	log.Println(helmTemplateCmd.String())
	err = helmTemplateCmd.Run()
	if err != nil {
		log.Printf("Error Templating: %s : due to %s", helmTemplateCmd.String(), helmTemplateOutputErr.String())
		return nil, err
	}

	// return the output
	return helmTemplateOutput.Bytes(), nil
}

// returns all the crds
func findCRDs(dir finder.Dir) (b []string) {
	var crds []string

	for _, output := range dir.Instance.Outputs {
		if output.APIVersion.Kind == "CustomResourceDefinition" {
			crds = append(crds, output.Name)
		}
	}
	return crds
}

func processCluster(cluster Cluster) (int, []string) {
	if len(cluster.Chart) > 0 {
		dir := finder.Dir{
			Instance: apiInstance,
		}
		data, err := templateChart("jf", cluster.Chart, cluster.ValuesFiles, cluster.ChartVersion)
		if err != nil {
			return UNKNOWN, nil
		}
		dir.Instance.Outputs, err = dir.Instance.IsVersioned(data)
		if err != nil {
			return UNKNOWN, nil
		}

		dir.Instance.FilterOutput()
		log.Println(dir.Instance.Outputs)
		crds := findCRDs(dir)
		if len(crds) > 0 {
			// 3 implies removed apis are included
			// 2 implies deprecated apis are included
			return FAILED, crds
		}
		return PASS, nil
	}
	return UNKNOWN, nil
}

func main() {

	_ = os.MkdirAll(workDirPath, 0775)

	initialiseApiInstance()
	argocd := parseArgocdAppsFile("/Users/siddharth.y/workspace/src/github.com/sedflix/argocd-apps-depreciation-detector/tests/argocd-apps-test.yaml")
	output := make(map[string][]string)
	for zoneName, zone := range argocd.Zones {
		log.Printf("zone: %s", zoneName)
		for name, cluster := range zone.Clusters {
			log.Printf("starting cluster %s", name)
			clusterstate, crds := processCluster(cluster)
			if clusterstate == FAILED {
				output[name] = crds
			}
			log.Printf("finished cluster %s with state %s", name, output[name])
		}
	}

	// write output to a file
	d, err := yaml.Marshal(&output)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	err = os.WriteFile("uat-nonpci.yaml", d, 0775)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
}
