package manifest

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gimlet-io/gimlet-cli/cmd/dashboard/config"
	"github.com/gimlet-io/gimlet-cli/pkg/commands/chart"
	"github.com/gimlet-io/gimlet-cli/pkg/dx"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"
	"github.com/urfave/cli/v2"
	giturl "github.com/whilp/git-urls"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/helmpath"
	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

var manifestConfigureCmd = cli.Command{
	Name:      "configure",
	Usage:     "Configures Helm chart values in a Gimlet manifest",
	UsageText: `gimlet manifest configure -f .gimlet/staging.yaml`,
	Action:    configure,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "file",
			Aliases:  []string{"f"},
			Usage:    "configuring an existing manifest file",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "schema",
			Aliases: []string{"s"},
			Usage:   "schema file to render, made for schema development",
		},
		&cli.StringFlag{
			Name:    "ui-schema",
			Aliases: []string{"u"},
			Usage:   "ui schema file to render, made for schema development",
		},
		&cli.StringFlag{
			Name:    "chart",
			Aliases: []string{"c"},
			Usage:   "Helm chart to deploy",
		},
	},
}

type manifestValues struct {
	App       string                 `yaml:"app" json:"app"`
	Env       string                 `yaml:"env" json:"env"`
	Namespace string                 `yaml:"namespace" json:"namespace"`
	Values    map[string]interface{} `yaml:"values" json:"values"`
}

func configure(c *cli.Context) error {
	manifestPath := c.String("file")
	manifestString, err := ioutil.ReadFile(manifestPath)
	if err != nil && !strings.Contains(err.Error(), "no such file or directory") {
		return fmt.Errorf("cannot read manifest file")
	}

	var m dx.Manifest
	if manifestString != nil {
		err = yaml.Unmarshal(manifestString, &m)
		if err != nil {
			return fmt.Errorf("cannot unmarshal manifest: %s", err)
		}

		for _, dep := range m.Dependencies {
			tfSpec := dep.Spec.(dx.TFSpec)
			var vars []*tfconfig.Variable
			variablesString, err := TfVariables(tfSpec.Module.Url)
			if err != nil {
				return fmt.Errorf("cannot read variables")
			}

			ParseVariables(variablesString, &vars)
			fmt.Println(vars)
		}
	} else {
		chartName, repoUrl, chartVersion, err := helmChartInfo(c.String("chart"))
		if err != nil {
			return fmt.Errorf("cannot get helm chart info: %s", err)
		}
		m = dx.Manifest{
			Chart: dx.Chart{
				Name:       chartName,
				Repository: repoUrl,
				Version:    chartVersion,
			},
		}
	}

	var tmpChartName string
	if strings.HasPrefix(m.Chart.Name, "git@") ||
		strings.Contains(m.Chart.Name, ".git") { // for https:// git urls
		tmpChartName, err = dx.CloneChartFromRepo(&m, "")
		if err != nil {
			return fmt.Errorf("cannot fetch chart from git %s", err.Error())
		}
		defer os.RemoveAll(tmpChartName)
	} else {
		tmpChartName = m.Chart.Name
	}

	values := manifestValues{
		App:       m.App,
		Env:       m.Env,
		Namespace: m.Namespace,
		Values:    m.Values,
	}
	valuesJson, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("cannot marshal values %s", err.Error())
	}

	var debugSchema, debugUISchema string
	if c.String("schema") != "" {
		debugSchemaBytes, err := ioutil.ReadFile(c.String("schema"))
		if err != nil {
			return fmt.Errorf("cannot read debugSchema file")
		}
		debugSchema = string(debugSchemaBytes)
	}
	if c.String("ui-schema") != "" {
		debugUISchemaBytes, err := ioutil.ReadFile(c.String("ui-schema"))
		if err != nil {
			return fmt.Errorf("cannot read debugUISchema file")
		}
		debugUISchema = string(debugUISchemaBytes)
	}

	yamlBytes, err := chart.ConfigureChart(
		tmpChartName,
		m.Chart.Repository,
		m.Chart.Version,
		valuesJson,
		debugSchema,
		debugUISchema,
	)
	if err != nil {
		return err
	}

	var configuredValues manifestValues
	err = yaml.Unmarshal(yamlBytes, &configuredValues)
	if err != nil {
		return fmt.Errorf("cannot unmarshal configured values %s", err.Error())
	}
	setManifestValues(&m, configuredValues)

	manifestString, err = yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("cannot marshal manifest")
	}

	err = ioutil.WriteFile(manifestPath, manifestString, 0666)
	if err != nil {
		return fmt.Errorf("cannot write values file %s", err)
	}
	fmt.Println("Manifest configuration succeeded")

	return nil
}

func helmChartInfo(chart string) (string, string, string, error) {
	var repoUrl, chartName, chartVersion string
	if chart != "" {
		if strings.HasPrefix(chart, "git@") {
			chartName = chart
		} else {
			chartString := chart
			chartLoader := action.NewShow(action.ShowChart)
			var settings = helmCLI.New()
			chartPath, err := chartLoader.ChartPathOptions.LocateChart(chartString, settings)
			if err != nil {
				return "", "", "", fmt.Errorf("could not load %s Helm chart", err.Error())
			}

			chart, err := loader.Load(chartPath)
			if err != nil {
				return "", "", "", fmt.Errorf("could not load %s Helm chart", err.Error())
			}

			chartName = chart.Name()
			chartVersion = chart.Metadata.Version

			chartParts := strings.Split(chartString, "/")
			if len(chartParts) != 2 {
				return "", "", "", fmt.Errorf("helm chart must be in the <repo>/<chart> format, try `helm repo ls` to find your chart")
			}
			repoName := chartParts[0]

			var helmRepo *repo.Entry
			f, err := repo.LoadFile(helmpath.ConfigPath("repositories.yaml"))
			if err != nil {
				return "", "", "", fmt.Errorf("cannot load Helm repositories")
			}
			for _, r := range f.Repositories {
				if r.Name == repoName {
					helmRepo = r
					break
				}
			}

			if helmRepo == nil {
				return "", "", "", fmt.Errorf("cannot find Helm repository %s", repoName)
			}
			repoUrl = helmRepo.URL
		}
		return chartName, repoUrl, chartVersion, nil
	}

	return config.DEFAULT_CHART_NAME, config.DEFAULT_CHART_REPO, config.DEFAULT_CHART_VERSION, nil
}

func setManifestValues(m *dx.Manifest, values manifestValues) {
	m.App = values.App
	m.Env = values.Env
	m.Namespace = values.Namespace
	m.Values = values.Values
}

func TfVariables(moduleUrl string) ([]byte, error) {
	gitAddress, err := giturl.Parse(moduleUrl)
	if err != nil {
		return nil, fmt.Errorf("cannot parse chart's git address: %s", err)
	}
	gitUrl := strings.ReplaceAll(moduleUrl, gitAddress.RawQuery, "")
	gitUrl = strings.ReplaceAll(gitUrl, "?", "")

	tmpDir, err := ioutil.TempDir("", "tfmodules")
	defer os.RemoveAll(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("cannot create tmp file: %s", err)
	}
	opts := &git.CloneOptions{
		URL: gitUrl,
	}

	repo, err := git.PlainClone(tmpDir, false, opts)
	if err != nil {
		return nil, fmt.Errorf("cannot clone chart git repo: %s", err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("cannot get worktree: %s", err)
	}

	params, _ := url.ParseQuery(gitAddress.RawQuery)
	if v, found := params["path"]; found {
		tmpDir = filepath.Join(tmpDir, v[0])
	}
	if v, found := params["sha"]; found {
		err = worktree.Checkout(&git.CheckoutOptions{
			Hash: plumbing.NewHash(v[0]),
		})
		if err != nil {
			return nil, fmt.Errorf("cannot checkout sha: %s", err)
		}
	}
	if v, found := params["tag"]; found {
		err = worktree.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewTagReferenceName(v[0]),
		})
		if err != nil {
			return nil, fmt.Errorf("cannot checkout tag: %s", err)
		}
	}
	if v, found := params["branch"]; found {
		err = worktree.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewRemoteReferenceName("origin", v[0]),
		})
		if err != nil {
			return nil, fmt.Errorf("cannot checkout branch: %s", err)
		}
	}

	f, err := os.ReadFile(filepath.Join(tmpDir, "variables.tf"))
	if err != nil {
		return nil, fmt.Errorf("cannot open file: %s", err)
	}

	return f, nil
}
