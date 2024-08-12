package server

import (
	"bytes"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gimlet-io/gimlet/cmd/dashboard/config"
	"github.com/gimlet-io/gimlet/cmd/dashboard/dynamicconfig"
	"github.com/gimlet-io/gimlet/pkg/dashboard/api"
	"github.com/gimlet-io/gimlet/pkg/dashboard/model"
	"github.com/gimlet-io/gimlet/pkg/dashboard/store"
	"github.com/gimlet-io/gimlet/pkg/dx"
	"github.com/gimlet-io/gimlet/pkg/git/customScm"
	"github.com/gimlet-io/gimlet/pkg/git/genericScm"
	"github.com/gimlet-io/gimlet/pkg/git/nativeGit"
	"github.com/gimlet-io/gimlet/pkg/gitops"
	"github.com/gimlet-io/gimlet/pkg/server/token"
	"github.com/gimlet-io/gimlet/pkg/stack"
	"github.com/gimlet-io/go-scm/scm"
	"github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	gitHttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/gorilla/securecookie"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type saveInfrastructureComponentsReq struct {
	Env                      string                 `json:"env"`
	InfrastructureComponents map[string]interface{} `json:"infrastructureComponents"`
}

func saveInfrastructureComponents(w http.ResponseWriter, r *http.Request) {
	var req saveInfrastructureComponentsReq
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		logrus.Errorf("cannot decode req: %s", err)
		http.Error(w, http.StatusText(400), 400)
		return
	}

	ctx := r.Context()
	db := r.Context().Value("store").(*store.Store)
	tokenManager := ctx.Value("tokenManager").(customScm.NonImpersonatedTokenManager)
	token, _, _ := tokenManager.Token()
	dynamicConfig := ctx.Value("dynamicConfig").(*dynamicconfig.DynamicConfig)
	user := ctx.Value("user").(*model.User)
	goScm := genericScm.NewGoScmHelper(dynamicConfig, nil)

	env, err := db.GetEnvironment(req.Env)
	if err != nil {
		logrus.Errorf("cannot get env: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	gitRepoCache, _ := ctx.Value("gitRepoCache").(*nativeGit.RepoCache)
	repo, tmpPath, err := gitRepoCache.InstanceForWrite(env.InfraRepo)
	defer os.RemoveAll(tmpPath)
	if err != nil {
		logrus.Errorf("cannot get repo: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var stackConfig *dx.StackConfig
	stackYamlPath := filepath.Join(req.Env, "stack.yaml")
	if env.RepoPerEnv {
		stackYamlPath = "stack.yaml"
	}

	stackConfig, err = stackYaml(repo, stackYamlPath)
	if err != nil {
		if strings.Contains(err.Error(), "file not found") {
			url := stack.DefaultStackURL
			latestTag, _ := stack.LatestVersion(url)
			if latestTag != "" {
				url = url + "?tag=" + latestTag
			}

			stackConfig = &dx.StackConfig{
				Stack: dx.StackRef{
					Repository: url,
				},
			}
		} else {
			logrus.Errorf("cannot get stack yaml from repo: %s", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	stackConfig.Config = req.InfrastructureComponents
	stackConfigBuff := bytes.NewBufferString("")
	e := yaml.NewEncoder(stackConfigBuff)
	e.SetIndent(2)
	err = e.Encode(stackConfig)
	if err != nil {
		logrus.Errorf("cannot serialize stack config: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	headBranch, err := nativeGit.HeadBranch(repo)
	if err != nil {
		logrus.Errorf("cannot get head branch: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	sourceBranch, err := GenerateBranchNameWithUniqueHash(fmt.Sprintf("gimlet-stack-change-%s", env.Name), 4)
	if err != nil {
		logrus.Errorf("cannot generate branch name: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	err = nativeGit.Branch(repo, fmt.Sprintf("refs/heads/%s", sourceBranch))
	if err != nil {
		logrus.Errorf("cannot checkout branch: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	err = os.WriteFile(filepath.Join(tmpPath, stackYamlPath), stackConfigBuff.Bytes(), nativeGit.Dir_RWX_RX_R)
	if err != nil {
		logrus.Errorf("cannot write file: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	err = stack.GenerateAndWriteFiles(*stackConfig, filepath.Join(tmpPath, stackYamlPath))
	if err != nil {
		logrus.Errorf("cannot generate and write files: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	err = StageCommitAndPush(repo, tmpPath, token, "[Gimlet] Updating components")
	if err != nil {
		logrus.Errorf("cannot stage commit and push: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	createdPR, _, err := goScm.CreatePR(token, env.InfraRepo, sourceBranch, headBranch,
		fmt.Sprintf("[Gimlet] `%s` infrastructure components change", env.Name),
		fmt.Sprintf("@%s is editing the infrastructure components on `%s`", user.Login, env.Name))
	if err != nil {
		logrus.Errorf("cannot create pr: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	gitRepoCache.Invalidate(env.InfraRepo)

	response := map[string]interface{}{
		"envName": env.Name,
		"createdPr": &api.PR{
			Sha:     createdPR.Sha,
			Link:    createdPR.Link,
			Title:   createdPR.Title,
			Branch:  createdPR.Source,
			Number:  createdPR.Number,
			Author:  createdPR.Author.Login,
			Created: int(createdPR.Created.Unix()),
			Updated: int(createdPR.Updated.Unix()),
		},
		"stackConfig": stackConfig,
	}

	responseString, err := json.Marshal(response)
	if err != nil {
		logrus.Errorf("cannot serialize stack config: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(responseString))
}

func bootstrapGitops(w http.ResponseWriter, r *http.Request) {
	bootstrapConfig := &api.GitopsBootstrapConfig{}
	err := json.NewDecoder(r.Body).Decode(&bootstrapConfig)
	if err != nil {
		logrus.Errorf("cannot decode bootstrap config: %s", err)
		http.Error(w, http.StatusText(400), 400)
		return
	}

	ctx := r.Context()
	perf := ctx.Value("perf").(*prometheus.HistogramVec)
	config := ctx.Value("config").(*config.Config)
	dynamicConfig := ctx.Value("dynamicConfig").(*dynamicconfig.DynamicConfig)
	tokenManager := ctx.Value("tokenManager").(customScm.NonImpersonatedTokenManager)
	gitServiceImpl := customScm.NewGitService(dynamicConfig)
	gitToken, gitUser, _ := tokenManager.Token()
	org := dynamicConfig.Org()

	dao := r.Context().Value("store").(*store.Store)
	environment, err := dao.GetEnvironment(bootstrapConfig.EnvName)
	if err != nil {
		logrus.Errorf("cannot get environment: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	environment.InfraRepo = bootstrapConfig.InfraRepo
	environment.AppsRepo = bootstrapConfig.AppsRepo
	if !strings.Contains(environment.InfraRepo, "/") {
		environment.InfraRepo = filepath.Join(org, environment.InfraRepo)
	}
	if !strings.Contains(environment.AppsRepo, "/") {
		environment.AppsRepo = filepath.Join(org, environment.AppsRepo)
	}

	environment.RepoPerEnv = bootstrapConfig.RepoPerEnv
	environment.KustomizationPerApp = bootstrapConfig.KusomizationPerApp
	err = dao.UpdateEnvironment(environment)
	if err != nil {
		logrus.Errorf("cannot update environment: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	user := ctx.Value("user").(*model.User)

	t0 := time.Now()
	_, err = AssureRepoExists(
		environment.InfraRepo,
		user.AccessToken,
		gitToken,
		user.Login,
		gitServiceImpl,
	)
	perf.WithLabelValues("gimlet_bootstrap_gitops_infra_repo_exists").Observe(float64(time.Since(t0).Seconds()))

	if err != nil {
		logrus.Errorf("cannot assure repo exists: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	t0 = time.Now()
	_, err = AssureRepoExists(
		environment.AppsRepo,
		user.AccessToken,
		gitToken,
		user.Login,
		gitServiceImpl,
	)
	perf.WithLabelValues("gimlet_bootstrap_gitops_apps_repo_exists").Observe(float64(time.Since(t0).Seconds()))

	if err != nil {
		logrus.Errorf("cannot assure repo exists: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	scmURL := dynamicConfig.ScmURL()
	gitRepoCache, _ := ctx.Value("gitRepoCache").(*nativeGit.RepoCache)

	t0 = time.Now()
	_, _, err = BootstrapEnv(
		gitRepoCache,
		gitServiceImpl,
		environment.Name,
		environment.InfraRepo,
		bootstrapConfig.RepoPerEnv,
		gitToken,
		true,
		true,
		false,
		false,
		scmURL,
	)
	perf.WithLabelValues("gimlet_bootstrap_gitops_bootstrap_infra_repo").Observe(float64(time.Since(t0).Seconds()))

	if err != nil {
		logrus.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	t0 = time.Now()
	_, _, err = BootstrapEnv(
		gitRepoCache,
		gitServiceImpl,
		environment.Name,
		environment.AppsRepo,
		bootstrapConfig.RepoPerEnv,
		gitToken,
		false,
		false,
		environment.KustomizationPerApp,
		true,
		scmURL,
	)
	perf.WithLabelValues("gimlet_bootstrap_gitops_bootstrap_apps_repo").Observe(float64(time.Since(t0).Seconds()))

	if err != nil {
		logrus.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	tokenStr, err := PrepApiKey(fmt.Sprintf(FluxApiKeyPattern, environment.Name), dao, config.Instance)
	if err != nil {
		logrus.Errorf("couldn't create user token %s", err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	t0 = time.Now()
	_, err = BootstrapNotifications(
		gitRepoCache,
		config.Host,
		tokenStr,
		environment,
		gitToken,
		gitUser,
	)
	perf.WithLabelValues("gimlet_bootstrap_gitops_bootstrap_notifications").Observe(float64(time.Since(t0).Seconds()))

	if err != nil {
		logrus.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	t0 = time.Now()
	err = installAgent(environment, gitRepoCache, config, dynamicConfig, gitToken, dao)
	perf.WithLabelValues("gimlet_bootstrap_gitops_install_agent").Observe(float64(time.Since(t0).Seconds()))

	if err != nil {
		logrus.Errorf("cannot install agent: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}

func BootstrapEnv(
	gitRepoCache *nativeGit.RepoCache,
	gitServiceImpl customScm.CustomGitService,
	envName string,
	repoName string,
	repoPerEnv bool,
	token string,
	shouldGenerateController bool,
	shouldGenerateDependencies bool,
	kustomizationPerApp bool,
	deployKeyCanWrite bool,
	scmURL string,
) (string, string, error) {
	repo, tmpPath, err := gitRepoCache.InstanceForWrite(repoName)
	defer os.RemoveAll(tmpPath)
	if err != nil {
		if strings.Contains(err.Error(), "remote repository is empty") {
			repo, tmpPath, err = initRepo(scmURL, repoName)
			defer os.RemoveAll(tmpPath)
			if err != nil {
				return "", "", fmt.Errorf("cannot init empty repo: %s", err)
			}
		} else {
			return "", "", fmt.Errorf("cannot get repo: %s", err)
		}
	}

	if repoPerEnv {
		envName = ""
	}
	headBranch, err := nativeGit.HeadBranch(repo)
	if err != nil {
		return "", "", fmt.Errorf("cannot get head branch: %s", err)
	}

	scmHost := strings.Split(scmURL, "://")[1]
	gitopsRepoFileName, publicKey, secretFileName, err := gitops.GenerateManifests(gitops.ManifestOpts{
		ShouldGenerateController:           shouldGenerateController,
		ShouldGenerateDependencies:         shouldGenerateDependencies,
		KustomizationPerApp:                kustomizationPerApp,
		Env:                                envName,
		SingleEnv:                          repoPerEnv,
		GitopsRepoPath:                     tmpPath,
		ShouldGenerateKustomizationAndRepo: true,
		ShouldGenerateDeployKey:            true,
		GitopsRepoUrl:                      fmt.Sprintf("git@%s:%s.git", scmHost, repoName),
		Branch:                             headBranch,
	})
	if err != nil {
		return "", "", fmt.Errorf("cannot generate manifest: %s", err)
	}

	err = StageCommitAndPush(repo, tmpPath, token, "[Gimlet] Bootstrapping")
	if err != nil {
		return "", "", fmt.Errorf("cannot stage commit and push: %s", err)
	}

	owner, repository := ParseRepo(repoName)
	err = gitServiceImpl.AddDeployKeyToRepo(
		owner,
		repository,
		token,
		"flux",
		publicKey,
		deployKeyCanWrite,
	)
	if err != nil {
		return "", "", fmt.Errorf("cannot add deploy key to repo: %s", err)
	}

	gitRepoCache.Invalidate(repoName)

	return gitopsRepoFileName, secretFileName, nil
}

func MigrateEnv(
	gitRepoCache *nativeGit.RepoCache,
	gitServiceImpl customScm.CustomGitService,
	envName string,
	oldRepoName string,
	newRepoName string,
	repoPerEnv bool,
	scmToken string,
	shouldGenerateController bool,
	shouldGenerateDependencies bool,
	kustomizationPerApp bool,
	deployKeyCanWrite bool,
	scmURL string,
	gitUser *model.User,
) (string, string, error) {
	repo, tmpPath, err := gitRepoCache.InstanceForWrite(oldRepoName)
	defer os.RemoveAll(tmpPath)
	if err != nil {
		return "", "", fmt.Errorf("cannot get repo: %s", err)
	}

	if repoPerEnv {
		envName = ""
	}
	headBranch, err := nativeGit.HeadBranch(repo)
	if err != nil {
		return "", "", fmt.Errorf("cannot get head branch: %s", err)
	}

	owner, repoName := scm.Split(oldRepoName)
	deployKeyName := fmt.Sprintf("deploy-key-%s.yaml", gitops.UniqueName(repoPerEnv, owner, repoName, envName))
	err = os.Remove(tmpPath + "/flux/" + deployKeyName)
	if err != nil {
		return "", "", fmt.Errorf("cannot remove: %s", err)
	}

	scmHost := strings.Split(scmURL, "://")[1]
	gitopsRepoFileName, publicKey, secretFileName, err := gitops.GenerateManifests(gitops.ManifestOpts{
		ShouldGenerateController:           shouldGenerateController,
		ShouldGenerateDependencies:         shouldGenerateDependencies,
		KustomizationPerApp:                kustomizationPerApp,
		Env:                                envName,
		SingleEnv:                          repoPerEnv,
		GitopsRepoPath:                     tmpPath,
		ShouldGenerateKustomizationAndRepo: true,
		ShouldGenerateDeployKey:            true,
		GitopsRepoUrl:                      fmt.Sprintf("git@%s:%s.git", scmHost, newRepoName),
		Branch:                             headBranch,
	})
	if err != nil {
		return "", "", fmt.Errorf("cannot generate manifest: %s", err)
	}

	err = stageCommitAndPush(repo, tmpPath, gitUser.Login, gitUser.Token, "[Gimlet] Migrating")
	if err != nil {
		return "", "", fmt.Errorf("cannot stage commit and push: %s", err)
	}

	head, _ := repo.Head()
	url := fmt.Sprintf("https://abc123:%s@github.com/%s.git", scmToken, newRepoName)
	err = nativeGit.NativeForcePushWithToken(
		url,
		tmpPath,
		head.Name().Short(),
	)
	if err != nil {
		return "", "", fmt.Errorf("cannot push to new repo: %s", err)
	}

	owner, repository := ParseRepo(newRepoName)
	err = gitServiceImpl.AddDeployKeyToRepo(
		owner,
		repository,
		scmToken,
		"flux",
		publicKey,
		deployKeyCanWrite,
	)
	if err != nil {
		return "", "", fmt.Errorf("cannot add deploy key to repo: %s", err)
	}

	gitRepoCache.Invalidate(oldRepoName)

	return gitopsRepoFileName, secretFileName, nil
}

func stageCommitAndPush(repo *git.Repository, tmpPath string, user string, password string, msg string) error {
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	err = worktree.AddWithOptions(&git.AddOptions{
		All: true,
	})
	if err != nil {
		return err
	}

	// Temporarily staging deleted files to git with a simple CLI command until the
	// following issue is not solved:
	// https://github.com/go-git/go-git/issues/223
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = tmpPath
	err = cmd.Run()
	if err != nil {
		return err
	}

	_, err = nativeGit.Commit(repo, msg)
	if err != nil {
		return err
	}

	err = nativeGit.PushWithBasicAuth(repo, user, password)
	if err != nil {
		return err
	}

	return nil
}

func initRepo(scmURL string, repoName string) (*git.Repository, string, error) {
	tmpPath, _ := ioutil.TempDir("", "gitops-")
	repo, err := git.PlainInit(tmpPath, false)
	if err != nil {
		return nil, tmpPath, fmt.Errorf("cannot init empty repo: %s", err)
	}
	w, err := repo.Worktree()
	if err != nil {
		return nil, tmpPath, fmt.Errorf("cannot init empty repo: %s", err)
	}
	err = nativeGit.StageFile(w, "", "README.md")
	if err != nil {
		return nil, tmpPath, fmt.Errorf("cannot init empty repo: %s", err)
	}
	_, err = nativeGit.Commit(repo, "Init")
	if err != nil {
		return nil, tmpPath, fmt.Errorf("cannot init empty repo: %s", err)
	}
	_, err = repo.CreateRemote(&gitConfig.RemoteConfig{
		Name: "origin",
		URLs: []string{fmt.Sprintf("%s/%s.git", scmURL, repoName)},
	})
	if err != nil {
		return nil, tmpPath, fmt.Errorf("cannot init empty repo: %s", err)
	}

	return repo, tmpPath, nil
}

func BootstrapNotifications(
	gitRepoCache *nativeGit.RepoCache,
	host string,
	gimletToken string,
	env *model.Environment,
	token string,
	gitUser string,
) (string, error) {
	repo, tmpPath, err := gitRepoCache.InstanceForWrite(env.AppsRepo)
	defer os.RemoveAll(tmpPath)
	if err != nil {
		return "", fmt.Errorf("cannot get repo: %s", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return "", err
	}

	w.Pull(&git.PullOptions{
		Auth: &gitHttp.BasicAuth{
			Username: gitUser,
			Password: token,
		},
		RemoteName: "origin",
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return "", fmt.Errorf("could not fetch: %s", err)
	}

	notificationsFileName, err := gitops.GenerateManifestProviderAndAlert(env, tmpPath, host, gimletToken)
	if err != nil {
		return "", fmt.Errorf("cannot generate notifications manifest: %s", err)
	}

	err = StageCommitAndPush(repo, tmpPath, token, "[Gimlet] Bootstrapping")
	if err != nil {
		return "", fmt.Errorf("cannot stage commit and push: %s", err)
	}

	gitRepoCache.Invalidate(env.AppsRepo)

	return notificationsFileName, nil
}

func AssureRepoExists(
	repoName string,
	userToken string,
	orgToken string,
	loggedInUser string,
	gitServiceImpl customScm.CustomGitService,
) (bool, error) {
	owner := ""
	parts := strings.Split(repoName, "/")
	if len(parts) == 2 {
		owner = parts[0]
		repoName = parts[1]
	}

	err := gitServiceImpl.CreateRepository(owner, repoName, loggedInUser, orgToken, userToken)
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, err
}

func StageCommitAndPush(repo *git.Repository, tmpPath string, token string, msg string) error {
	_, err := stageCommitAndPushWithHash(repo, tmpPath, token, msg)
	return err
}

func stageCommitAndPushWithHash(repo *git.Repository, tmpPath string, token string, msg string) (string, error) {
	worktree, err := repo.Worktree()
	if err != nil {
		return "", err
	}

	err = worktree.AddWithOptions(&git.AddOptions{
		All: true,
	})
	if err != nil {
		return "", err
	}

	// Temporarily staging deleted files to git with a simple CLI command until the
	// following issue is not solved:
	// https://github.com/go-git/go-git/issues/223
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = tmpPath
	err = cmd.Run()
	if err != nil {
		return "", err
	}

	hash, err := nativeGit.Commit(repo, msg)
	if err != nil {
		return "", err
	}

	err = nativeGit.PushWithToken(repo, token)

	return hash, err
}

func StageCommitAndPushGimletFolder(repo *git.Repository, tmpPath string, token string, msg string) error {
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	err = worktree.AddWithOptions(&git.AddOptions{
		Path: ".gimlet",
	})
	if err != nil {
		return err
	}

	_, err = nativeGit.Commit(repo, msg)
	if err != nil {
		return err
	}

	return nativeGit.PushWithToken(repo, token)
}

func PrepApiKey(
	name string,
	dao *store.Store,
	issuer string,
) (string, error) {
	user := &model.User{
		Login:  name,
		Secret: base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(32)),
	}

	err := dao.CreateUser(user)
	if err != nil {
		return "", fmt.Errorf("cannot create user %s: %s", user.Login, err)
	}

	token := token.New(token.UserToken, user.Login, issuer)
	tokenStr, err := token.Sign(user.Secret)
	if err != nil {
		return "", fmt.Errorf("couldn't create user token %s", err)
	}

	return tokenStr, nil
}

func installAgent(
	env *model.Environment,
	gitRepoCache *nativeGit.RepoCache,
	config *config.Config,
	dynamicConfig *dynamicconfig.DynamicConfig,
	gitToken string,
	dao *store.Store,
) error {
	repo, tmpPath, err := gitRepoCache.InstanceForWrite(env.InfraRepo)
	defer os.RemoveAll(tmpPath)
	if err != nil {
		return fmt.Errorf("cannot get repo: %s", err)
	}

	serverComponents := ComponentOpts{
		Agent:                 true,
		ImageBuilder:          true,
		ContainerizedRegistry: true,
		SealedSecrets:         true,
	}
	err = PrepInfraComponentManifests(env, tmpPath, repo, config, dynamicConfig, serverComponents, dao)
	if err != nil {
		return fmt.Errorf("cannot configure agent: %s", err)
	}

	err = StageCommitAndPush(repo, tmpPath, gitToken, "[Gimlet] Installing agent")
	if err != nil {
		return fmt.Errorf("cannot stage commit and push: %s", err)
	}

	gitRepoCache.Invalidate(env.InfraRepo)

	return nil
}

type ComponentOpts struct {
	Agent                 bool
	ImageBuilder          bool
	ContainerizedRegistry bool
	SealedSecrets         bool
}

func PrepInfraComponentManifests(
	env *model.Environment,
	tmpPath string,
	repo *git.Repository,
	config *config.Config,
	dynamicConfig *dynamicconfig.DynamicConfig,
	opts ComponentOpts,
	dao *store.Store,
) error {
	stackYamlPath := filepath.Join(env.Name, "stack.yaml")
	if env.RepoPerEnv {
		stackYamlPath = "stack.yaml"
	}

	stackConfig, err := stackYaml(repo, stackYamlPath)
	if err != nil {
		if strings.Contains(err.Error(), "file not found") ||
			strings.Contains(err.Error(), "cannot get head commit") {
			url := stack.DefaultStackURL
			// latestTag, _ := stack.LatestVersion(url)
			// if latestTag != "" {
			// 	url = url + "?tag=" + latestTag
			// }

			stackConfig = &dx.StackConfig{
				Stack: dx.StackRef{
					Repository: url,
				},
				Config: map[string]interface{}{},
			}
		} else {
			return fmt.Errorf("cannot get stack yaml from repo: %s", err)
		}
	}

	if opts.Agent {
		agentKey, err := PrepApiKey(fmt.Sprintf(AgentApiKeyPattern, env.Name), dao, config.Instance)
		if err != nil {
			return fmt.Errorf("cannot prep agent key: %s", err)
		}

		stackConfig.Config["gimletAgent"] = map[string]interface{}{
			"enabled":          true,
			"environment":      env.Name,
			"agentKey":         agentKey,
			"dashboardAddress": config.ApiHost,
		}
	}
	if opts.ImageBuilder {
		stackConfig.Config["imageBuilder"] = map[string]interface{}{
			"enabled": true,
		}
	}
	if opts.ContainerizedRegistry {
		stackConfig.Config["containerizedRegistry"] = map[string]interface{}{
			"enabled": true,
			"credentials": map[string]interface{}{
				"url": "127.0.0.1:32447",
			},
		}
	}
	if opts.SealedSecrets {
		stackConfig.Config["sealedSecrets"] = map[string]interface{}{
			"enabled": true,
		}
	}

	stackConfigBuff := bytes.NewBufferString("")
	e := yaml.NewEncoder(stackConfigBuff)
	e.SetIndent(2)
	err = e.Encode(stackConfig)
	if err != nil {
		return fmt.Errorf("cannot serialize stack config: %s", err)
	}

	err = os.WriteFile(filepath.Join(tmpPath, stackYamlPath), stackConfigBuff.Bytes(), nativeGit.Dir_RWX_RX_R)
	if err != nil {
		return fmt.Errorf("cannot write file: %s", err)
	}

	err = stack.GenerateAndWriteFiles(*stackConfig, filepath.Join(tmpPath, stackYamlPath))
	if err != nil {
		return fmt.Errorf("cannot generate and write files: %s", err)
	}

	return nil
}

func ParseRepo(repoName string) (string, string) {
	owner := strings.Split(repoName, "/")[0]
	repo := strings.Split(repoName, "/")[1]
	return owner, repo
}

func EnsureGimletCloudSettings(
	dao *store.Store,
	gitRepoCache *nativeGit.RepoCache,
	tokenManager customScm.NonImpersonatedTokenManager,
	gitUser *model.User,
	envName string,
	sealedSecretsCertificate []byte,
	instance string,
) {
	_, err := dao.KeyValue(model.EnsuredCustomRegistry)
	if err == nil { // custom registry is ensured
		return
	}

	env, err := dao.GetEnvironment(envName)
	if err != nil {
		logrus.Errorf("could not ensure custom registry: %s", err)
		return
	}

	repo, tmpPath, err := gitRepoCache.InstanceForWrite(env.InfraRepo)
	defer os.RemoveAll(tmpPath)
	if err != nil {
		logrus.Errorf("could not get infra repo: %s", err)
		return
	}

	stackYamlPath := filepath.Join(env.Name, "stack.yaml")
	if env.RepoPerEnv {
		stackYamlPath = "stack.yaml"
	}

	stackConfig, err := stackYaml(repo, stackYamlPath)
	if err != nil {
		logrus.Errorf("could not get infra repo %s: %s", env.InfraRepo, err)
		return
	}

	key, err := parseKey(sealedSecretsCertificate)
	if err != nil {
		logrus.Errorf("cannot parse public key: %s", err)
		return
	}

	creds, err := sealValue(key, os.Getenv("GIMLET_CLOUD_REGISTRY_CREDS"))
	if err != nil {
		logrus.Errorf("cannot seal item: %s", err)
		return
	}
	login := os.Getenv("GIMLET_CLOUD_REGISTRY_LOGIN")
	cert, err := sealValue(key, os.Getenv("GIMLET_CLOUD_REGISTRY_CERT"))
	if err != nil {
		logrus.Errorf("cannot seal item: %s", err)
		return
	}

	stackConfig.Config["customRegistry"] = map[string]interface{}{
		"credentials": map[string]interface{}{
			"encryptedDockerconfigjson": creds,
			"login":                     login,
			"url":                       "registry.gimlet:30003",
		},
		"displayName":    "Gimlet Registry",
		"selfSignedCert": cert,
	}
	stackConfig.Config["existingIngress"] = map[string]interface{}{
		"enabled": true,
		"host":    fmt.Sprintf("-%s.gimlet.app", instance),
		"ingressAnnotations": map[string]interface{}{
			"kubernetes.io/ingress.class":    "nginx",
			"cert-manager.io/cluster-issuer": "letsencrypt",
		},
	}

	stackConfigBuff := bytes.NewBufferString("")
	e := yaml.NewEncoder(stackConfigBuff)
	e.SetIndent(2)
	err = e.Encode(stackConfig)
	if err != nil {
		logrus.Errorf("cannot serialize stack config: %s", err)
		return
	}

	err = os.WriteFile(filepath.Join(tmpPath, stackYamlPath), stackConfigBuff.Bytes(), nativeGit.Dir_RWX_RX_R)
	if err != nil {
		logrus.Errorf("cannot write file %s: %s", stackYamlPath, err)
		return
	}

	err = stack.GenerateAndWriteFiles(*stackConfig, filepath.Join(tmpPath, stackYamlPath))
	if err != nil {
		logrus.Errorf("cannot generate and write files: %s", err)
		return
	}

	if env.BuiltIn {
		err = stageCommitAndPush(repo, tmpPath, gitUser.Login, gitUser.Token, "[Gimlet] Gimlet Cloud settings")
		if err != nil {
			logrus.Errorf("cannot stage commit and push: %s", err)
			return
		}
	} else {
		token, _, _ := tokenManager.Token()
		err = StageCommitAndPush(repo, tmpPath, token, "[Gimlet] Gimlet Cloud settings")
		if err != nil {
			logrus.Errorf("cannot stage commit and push: %s", err)
			return
		}
	}

	// this should happen only once
	_ = dao.SaveKeyValue(&model.KeyValue{
		Key:   model.EnsuredCustomRegistry,
		Value: "true",
	})
}
