package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gimlet-io/gimlet/cmd/dashboard/config"
	"github.com/gimlet-io/gimlet/pkg/dashboard/gitops"
	"github.com/gimlet-io/gimlet/pkg/dashboard/imageBuild"
	"github.com/gimlet-io/gimlet/pkg/dashboard/model"
	"github.com/gimlet-io/gimlet/pkg/dashboard/server/streaming"
	"github.com/gimlet-io/gimlet/pkg/dashboard/store"
	"github.com/gimlet-io/gimlet/pkg/dx"
	"github.com/gimlet-io/gimlet/pkg/git/customScm"
	"github.com/gimlet-io/gimlet/pkg/git/nativeGit"
	"github.com/gimlet-io/go-scm/scm"
	"github.com/go-git/go-git/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

type ByCreated []*dx.Release

func (a ByCreated) Len() int           { return len(a) }
func (a ByCreated) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByCreated) Less(i, j int) bool { return a[i].Created < a[j].Created }

func getReleases(w http.ResponseWriter, r *http.Request) {
	var since, until *time.Time
	var app, env, gitRepo string
	var reverse bool
	limit := 10
	ctx := r.Context()

	params := r.URL.Query()
	if val, ok := params["limit"]; ok {
		l, err := strconv.Atoi(val[0])
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest)+" - "+err.Error(), http.StatusBadRequest)
			return
		}
		limit = l
	}

	if val, ok := params["reverse"]; ok {
		r, err := strconv.ParseBool(val[0])
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest)+" - "+err.Error(), http.StatusBadRequest)
			return
		}
		reverse = r
	}

	if val, ok := params["since"]; ok {
		t, err := time.Parse(time.RFC3339, val[0])
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest)+" - "+err.Error(), http.StatusBadRequest)
			return
		}
		since = &t
	}
	if since == nil {
		// limiting query scope
		// without these, for apps released just once, the whole history would be traversed
		config := ctx.Value("config").(*config.Config)
		t := time.Now().Add(-1 * time.Hour * 24 * time.Duration(config.ReleaseHistorySinceDays))
		since = &t
	}
	if val, ok := params["until"]; ok {
		t, err := time.Parse(time.RFC3339, val[0])
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest)+" - "+err.Error(), http.StatusBadRequest)
			return
		}
		until = &t
	}

	if val, ok := params["app"]; ok {
		app = val[0]
	}
	if val, ok := params["env"]; ok {
		env = val[0]
	} else {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "env parameter is mandatory"), http.StatusBadRequest)
		return
	}
	if val, ok := params["git-repo"]; ok {
		gitRepo = val[0]
	}

	gitopsRepoCache := ctx.Value("gitRepoCache").(*nativeGit.RepoCache)

	store := r.Context().Value("store").(*store.Store)
	repoName, repoPerEnv, err := gitopsRepoForEnv(store, env)
	if err != nil {
		logrus.Error(err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if repoName == "" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
		return
	}

	repo, pathToCleanUp, err := gitopsRepoCache.InstanceForWriteWithHistory(repoName) // using a copy of the repo to avoid concurrent map writes error
	defer gitopsRepoCache.CleanupWrittenRepo(pathToCleanUp)
	if err != nil {
		logrus.Errorf("cannot get gitops repo for write: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	perf := ctx.Value("perf").(*prometheus.HistogramVec)
	releases, err := gitops.Releases(repo, app, env, repoPerEnv, since, until, limit, gitRepo, perf)
	if err != nil {
		logrus.Errorf("cannot get releases: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if reverse {
		sort.Sort(ByCreated(releases))
	}

	for _, r := range releases {
		r.GitopsRepo = repoName

		gitopsCommitStatus, gitopsCommitStatusDesc, gitopsCommitCreated := gitopsCommitMetasFromHash(store, r.GitopsRef)
		r.GitopsCommitStatus = gitopsCommitStatus
		r.GitopsCommitStatusDesc = gitopsCommitStatusDesc
		r.GitopsCommitCreated = gitopsCommitCreated
	}

	releasesStr, err := json.Marshal(releases)
	if err != nil {
		logrus.Errorf("cannot serialize artifacts: %s", err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(releasesStr)
}

func getStatus(w http.ResponseWriter, r *http.Request) {
	var app, env string

	params := r.URL.Query()
	if val, ok := params["app"]; ok {
		app = val[0]
	}
	if val, ok := params["env"]; ok {
		env = val[0]
	} else {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "env parameter is mandatory"), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	gitopsRepoCache := ctx.Value("gitRepoCache").(*nativeGit.RepoCache)
	perf := ctx.Value("perf").(*prometheus.HistogramVec)

	db := r.Context().Value("store").(*store.Store)
	repoName, repoPerEnv, err := gitopsRepoForEnv(db, env)
	if err != nil {
		logrus.Error(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var appReleases map[string]*dx.Release
	gitopsRepoCache.PerformActionWithHistory(repoName, func(repo *git.Repository) {
		appReleases, err = gitops.Status(repo, app, env, repoPerEnv, perf)
	})
	if err != nil {
		logrus.Errorf("cannot get status: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	for _, release := range appReleases {
		if release != nil {
			release.GitopsRepo = repoName
			//release.Created = TODO Get githelper.Releases for each app with limit 1 - could be terribly slow
		}
	}

	appReleasesString, err := json.Marshal(appReleases)
	if err != nil {
		logrus.Errorf("cannot serialize app releases: %s", err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(appReleasesString)
}

func gitopsRepoForEnv(db *store.Store, env string) (string, bool, error) {
	envsFromDB, err := db.GetEnvironments()
	if err != nil {
		return "", false, fmt.Errorf("cannot get environments from database: %s", err)
	}

	for _, e := range envsFromDB {
		if e.Name == env {
			return e.AppsRepo, e.RepoPerEnv, nil
		}
	}
	return "", false, fmt.Errorf("no such environment: %s", env)
}

func release(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	store := ctx.Value("store").(*store.Store)
	user := ctx.Value("user").(*model.User)

	body, _ := ioutil.ReadAll(r.Body)
	var releaseRequest dx.ReleaseRequest
	err := json.NewDecoder(bytes.NewReader(body)).Decode(&releaseRequest)
	if err != nil {
		logrus.Errorf("cannot decode release request: %s", err)
		http.Error(w, http.StatusText(400), 400)
		return
	}

	if releaseRequest.Env == "" {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "env parameter is mandatory"), http.StatusBadRequest)
		return
	}

	if releaseRequest.ArtifactID == "" {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "artifact parameter is mandatory"), http.StatusBadRequest)
		return
	}

	artifactEvent, err := store.Artifact(releaseRequest.ArtifactID)
	if err != nil {
		http.Error(w, fmt.Sprintf("%s - cannot find artifact with id %s", http.StatusText(http.StatusNotFound), releaseRequest.ArtifactID), http.StatusNotFound)
		return
	}
	artifact, err := model.ToArtifact(artifactEvent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var imageBuildRequest *dx.ImageBuildRequest
	for _, manifest := range artifact.Environments {
		manifest.PrepPreview("not-needed-in-creating-deploy-requests")
		vars := artifact.CollectVariables()
		vars["APP"] = manifest.App
		err = manifest.ResolveVars(vars)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if manifest.Env != releaseRequest.Env {
			continue
		}
		if manifest.App != releaseRequest.App {
			continue
		}

		strategy := gitops.ExtractImageStrategy(manifest)
		if strategy == "buildpacks" || strategy == "dockerfile" {
			// events, err := store.EventsForRepoAndSha(artifact.Version.RepositoryName, artifact.Version.SHA)
			// if err != nil {
			// 	http.Error(w, err.Error(), http.StatusInternalServerError)
			// 	return
			// }

			// if imageHasBeenBuilt(events) {
			// 	break
			// }

			vars := artifact.CollectVariables()
			vars["APP"] = releaseRequest.App
			imageRepository, imageTag, dockerfile, registry := gitops.ExtractImageRepoTagDockerfileAndRegistry(manifest, vars)
			// Image push happens inside the cluster, pull is handled by the kubelet that doesn't speak cluster local addresses
			imageRepository = strings.ReplaceAll(imageRepository, "127.0.0.1:32447", "registry.infrastructure.svc.cluster.local:5000")
			imageBuildRequest = &dx.ImageBuildRequest{
				Env:         releaseRequest.Env,
				App:         releaseRequest.App,
				Sha:         artifact.Version.SHA,
				ArtifactID:  releaseRequest.ArtifactID,
				TriggeredBy: user.Login,
				Image:       imageRepository,
				Tag:         imageTag,
				Dockerfile:  dockerfile,
				Strategy:    strategy,
				Registry:    registry,
			}
			break
		}
	}

	var event *model.Event
	if imageBuildRequest != nil {
		gitRepoCache, _ := ctx.Value("gitRepoCache").(*nativeGit.RepoCache)
		agentHub, _ := ctx.Value("agentHub").(*streaming.AgentHub)

		event, err = imageBuild.TriggerImagebuild(gitRepoCache, agentHub, store, artifact, imageBuildRequest)
		if err != nil {
			logrus.Errorf("could not trigger image build: %s", err.Error())
			http.Error(w, "could not trigger image build", http.StatusInternalServerError)
			return
		}
	} else {
		event, err = releaseRequestEvent(releaseRequest, artifactEvent, user.Login)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		event, err = store.CreateEvent(event)
		if err != nil {
			http.Error(w, fmt.Sprintf("%s - cannot save release request: %s", http.StatusText(http.StatusInternalServerError), err), http.StatusInternalServerError)
			return
		}
	}

	eventIDBytes, _ := json.Marshal(map[string]string{
		"id":   event.ID,
		"type": event.Type,
	})

	w.WriteHeader(http.StatusCreated)
	w.Write(eventIDBytes)
}

func imageHasBeenBuilt(events []*model.Event) bool {
	for _, e := range events {
		if e.Type == model.ImageBuildRequestedEvent &&
			e.Status == model.Success.String() {
			return true
		}
	}

	return false
}

func releaseRequestEvent(releaseRequest dx.ReleaseRequest, artifactEvent *model.Event, login string) (*model.Event, error) {
	releaseRequestStr, err := json.Marshal(dx.ReleaseRequest{
		Env:         releaseRequest.Env,
		App:         releaseRequest.App,
		ArtifactID:  releaseRequest.ArtifactID,
		TriggeredBy: login,
	})
	if err != nil {
		return nil, fmt.Errorf("%s - cannot serialize release request: %s", http.StatusText(http.StatusInternalServerError), err)
	}

	event := &model.Event{
		Type:       model.ReleaseRequestedEvent,
		Blob:       string(releaseRequestStr),
		Repository: artifactEvent.Repository,
		SHA:        artifactEvent.SHA,
	}

	return event, nil
}

func performRollback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	store := ctx.Value("store").(*store.Store)
	user := ctx.Value("user").(*model.User)

	params := r.URL.Query()
	var env, app, targetSHA string
	if val, ok := params["env"]; ok {
		env = val[0]
	} else {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "env parameter is mandatory"), http.StatusBadRequest)
		return
	}
	if val, ok := params["app"]; ok {
		app = val[0]
	} else {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "app parameter is mandatory"), http.StatusBadRequest)
		return
	}
	if val, ok := params["sha"]; ok {
		targetSHA = val[0]
	} else {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "sha parameter is mandatory"), http.StatusBadRequest)
		return
	}

	rollbackRequestStr, err := json.Marshal(dx.RollbackRequest{
		Env:         env,
		App:         app,
		TargetSHA:   targetSHA,
		TriggeredBy: user.Login,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("%s - cannot serialize rollback request: %s", http.StatusText(http.StatusInternalServerError), err), http.StatusInternalServerError)
		return
	}

	event, err := store.CreateEvent(&model.Event{
		Type: model.RollbackRequestedEvent,
		Blob: string(rollbackRequestStr),
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("%s - cannot save rollback request: %s", http.StatusText(http.StatusInternalServerError), err), http.StatusInternalServerError)
		return
	}

	eventIDBytes, _ := json.Marshal(map[string]string{
		"id": event.ID,
	})

	w.WriteHeader(http.StatusCreated)
	w.Write(eventIDBytes)
}

func delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := ctx.Value("user").(*model.User)
	gitopsRepoCache := ctx.Value("gitRepoCache").(*nativeGit.RepoCache)

	params := r.URL.Query()
	var env, app string
	if val, ok := params["env"]; ok {
		env = val[0]
	} else {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "env parameter is mandatory"), http.StatusBadRequest)
		return
	}
	if val, ok := params["app"]; ok {
		app = val[0]
	} else {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "app parameter is mandatory"), http.StatusBadRequest)
		return
	}

	store := r.Context().Value("store").(*store.Store)
	envFromStore, err := store.GetEnvironment(env)
	if err != nil {
		logrus.Error(err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	repo, pathToCleanUp, err := gitopsRepoCache.InstanceForWriteWithHistory(envFromStore.AppsRepo)
	defer gitopsRepoCache.CleanupWrittenRepo(pathToCleanUp)
	if err != nil {
		logrus.Errorf("cannot get gitops repo for write: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	path := filepath.Join(env, app)
	if envFromStore.RepoPerEnv {
		path = app
	}

	if envFromStore.KustomizationPerApp {
		kustomizationFilePath := filepath.Join(env, "flux", fmt.Sprintf("kustomization-%s.yaml", app))
		if envFromStore.RepoPerEnv {
			kustomizationFilePath = filepath.Join("flux", fmt.Sprintf("kustomization-%s.yaml", app))
		}
		err := nativeGit.DelFile(repo, kustomizationFilePath)
		if err != nil {
			logrus.Errorf("cannot delete kustomization file: %s", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	err = nativeGit.DelDir(repo, path)
	if err != nil {
		logrus.Errorf("cannot delete release: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	empty, err := nativeGit.NothingToCommit(repo)
	if err != nil {
		logrus.Errorf("cannot determine git status: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if empty {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
		return
	}

	gitMessage := fmt.Sprintf("[Gimlet] %s/%s deleted by %s", env, app, user.Login)
	gitopsCommitSha, err := nativeGit.Commit(repo, gitMessage)
	if err != nil {
		logrus.Errorf("could not delete: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	config := ctx.Value("config").(*config.Config)

	t0 := time.Now().UnixNano()
	head, _ := repo.Head()
	tokenManager := ctx.Value("tokenManager").(customScm.NonImpersonatedTokenManager)
	token, _, _ := tokenManager.Token()
	owner, _ := scm.Split(envFromStore.AppsRepo)
	gitUser := ctx.Value("gitUser").(*model.User)

	url := fmt.Sprintf("https://abc123:%s@github.com/%s.git", token, envFromStore.AppsRepo)
	if owner == "builtin" {
		url = fmt.Sprintf("http://%s:%s@%s/%s", gitUser.Login, gitUser.Token, config.GitHost, envFromStore.AppsRepo)
	}
	err = nativeGit.NativePushWithToken(
		url,
		pathToCleanUp,
		head.Name().Short(),
	)
	if err != nil {
		logrus.Errorf("could not push: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	logrus.Infof("Pushing took %d", (time.Now().UnixNano()-t0)/1000/1000)

	gitopsRepoCache.Invalidate(envFromStore.AppsRepo)

	var gc *model.GitopsCommit
	for retries := 10; retries > 0; retries-- {
		gc, err = store.GitopsCommit(gitopsCommitSha)
		if err != nil {
			logrus.Errorf("could not get gitops commit: %s", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if gc != nil && gc.Status == model.ReconciliationSucceeded {
			break
		}
		time.Sleep(5 * time.Second)
	}
	if gc == nil || gc.Status != model.ReconciliationSucceeded {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}

func getEventReleaseTrack(w http.ResponseWriter, r *http.Request) {
	var id string

	params := r.URL.Query()

	if val, ok := params["id"]; ok {
		id = val[0]
	} else {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "id parameter is mandatory"), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	store := ctx.Value("store").(*store.Store)
	event, err := store.Event(id)
	if err == sql.ErrNoRows {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	} else if err != nil {
		logrus.Errorf("cannot get event: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}

	results := dxResults(store, event)
	statusBytes, _ := json.Marshal(dx.ReleaseStatus{
		Type:       event.Type,
		Status:     event.Status,
		StatusDesc: event.StatusDesc,
		Results:    results,
	})

	w.WriteHeader(http.StatusOK)
	w.Write(statusBytes)
}

func dxResults(store *store.Store, event *model.Event) []dx.Result {
	results := []dx.Result{}
	for _, result := range event.Results {
		if result.TriggeredDeployRequestID != "" {
			results = append(results, dx.Result{
				TriggeredDeployRequestID: result.TriggeredDeployRequestID,
			})
			continue
		}

		gitopsCommitStatus, gitopsCommitStatusDesc, _ := gitopsCommitMetasFromHash(store, result.GitopsRef)
		if event.Type == "rollback" {
			results = append(results, dx.Result{
				Hash:                   result.GitopsRef,
				Status:                 result.Status.String(),
				GitopsCommitStatus:     gitopsCommitStatus,
				GitopsCommitStatusDesc: gitopsCommitStatusDesc,
				StatusDesc:             result.StatusDesc,
				App:                    result.RollbackRequest.App,
				Env:                    result.RollbackRequest.Env,
			})
			continue
		}
		var app, env string
		if result.Manifest != nil {
			env = result.Manifest.Env
			app = result.Manifest.App
		}
		results = append(results, dx.Result{
			Env:                    env,
			App:                    app,
			Hash:                   result.GitopsRef,
			Status:                 result.Status.String(),
			GitopsCommitStatus:     gitopsCommitStatus,
			GitopsCommitStatusDesc: gitopsCommitStatusDesc,
			StatusDesc:             result.StatusDesc,
		})
	}
	return results
}

func getEventArtifactTrack(w http.ResponseWriter, r *http.Request) {
	var id string

	params := r.URL.Query()

	if val, ok := params["artifactId"]; ok {
		id = val[0]
	} else {
		http.Error(w, fmt.Sprintf("%s: %s", http.StatusText(http.StatusBadRequest), "id parameter is mandatory"), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	store := ctx.Value("store").(*store.Store)
	event, err := store.EventArtifactTrack(id)
	if err == sql.ErrNoRows {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	} else if err != nil {
		logrus.Errorf("cannot get event: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}

	results := []dx.Result{}
	for _, result := range event.Results {
		gitopsCommitStatus, gitopsCommitStatusDesc, _ := gitopsCommitMetasFromHash(store, result.GitopsRef)
		results = append(results, dx.Result{
			App:                    result.Manifest.App,
			Hash:                   result.GitopsRef,
			Status:                 result.Status.String(),
			GitopsCommitStatus:     gitopsCommitStatus,
			GitopsCommitStatusDesc: gitopsCommitStatusDesc,
			Env:                    result.Manifest.Env,
			StatusDesc:             result.StatusDesc,
		})
	}

	statusBytes, _ := json.Marshal(dx.ReleaseStatus{
		Status:     event.Status,
		StatusDesc: event.StatusDesc,
		Results:    results,
	})

	w.WriteHeader(http.StatusOK)
	w.Write(statusBytes)
}

func gitopsCommitMetasFromHash(store *store.Store, gitopsRef string) (string, string, int64) {
	if gitopsRef == "" {
		return "", "", 0
	}
	gitopsCommit, err := store.GitopsCommit(gitopsRef)
	if err != nil {
		logrus.Warnf("cannot get gitops commit: %s", err)
		return "", "", 0
	}
	if gitopsCommit == nil {
		return "", "", 0
	}

	return gitopsCommit.Status, gitopsCommit.StatusDesc, gitopsCommit.Created
}
