import React, { Component } from 'react';
import './app.css';
import Nav from "./components/nav/nav";
import StreamingBackend from "./streamingBackend";
import { createStore } from 'redux';
import { rootReducer } from './redux/redux';
import { BrowserRouter as Router, Redirect, Route, Switch, withRouter } from "react-router-dom";
import GimletClient from "./client/client";
import Repositories from "./views/repositories/repositories";
import APIBackend from "./apiBackend";
import Profile from "./views/profile/profile";
import Settings from "./views/settings/settings";
import Repo from "./views/repo/repo";
import { CommitView } from "./views/repo/commitView";
import { RepoSettingsView } from "./views/repo/settingsView";
import { PreviewView } from "./views/repo/previewView";
import LoginPage from './views/login/loginPage';
import EnvConfig from './views/envConfig/envConfig'
import { DeployWizzard } from './views/deployWizzard/deployWizzard'
import RepositoryWizard from './views/repositoryWizard/repositoryWizard';
import Environments from './views/environments/environments'
import Environment from './views/environment/environment';
import PopUpWindow from './popUpWindow';
import Footer from './views/footer/footer';
import {
  ACTION_TYPE_USER,
  ACTION_TYPE_SETTINGS,
} from "./redux/redux";
import Posthog from './posthog';
import './style.css'
import GithubIntegration from './views/githubIntegration';

export default class App extends Component {
  constructor(props) {
    super(props);

    const store = createStore(rootReducer);
    const gimletClient = new GimletClient(
      (response) => {
        if (response.status === 401) {
          if (!window.location.pathname.includes("/login")) {
            localStorage.setItem('redirect', window.location.pathname);
            window.location.replace("/login");
          }
        } else {
          console.log(`${response.status}: ${response.statusText} on ${response.path}`);
        }
      }
    );

    this.state = {
      store: store,
      gimletClient: gimletClient
    }
  }

  componentDidMount() {
    this.state.gimletClient.getUser()
      .then(data => {
        this.state.store.dispatch({ type: ACTION_TYPE_USER, payload: data });
        this.setState({
          userLoaded: true,
          authenticated: true
        });
        this.state.gimletClient.getSettings()
          .then(data => {
            this.state.store.dispatch({ type: ACTION_TYPE_SETTINGS, payload: data });
            this.setState({ settings: data });
          });
      }, () => {
        this.setState({
          userLoaded: true,
        });
      });
  }

  render() {
    const { store, gimletClient } = this.state;

    const NavBar = withRouter(props => <Nav {...props} store={store} />);
    const APIBackendWithLocation = withRouter(
      props => <APIBackend {...props} store={store} gimletClient={gimletClient} />
    );
    const StreamingBackendWithLocation = withRouter(props => <StreamingBackend {...props} store={store} />);
    const RepoWithRouting = withRouter(props => <Repo {...props} store={store} gimletClient={gimletClient} />);
    const RepositoriesWithRouting = withRouter(props => <Repositories {...props} store={store} gimletClient={gimletClient} />);
    const EnvironmentsWithRouting = withRouter(props => <Environments {...props} store={store} gimletClient={gimletClient} />);
    const EnvironmentWithRouting = withRouter(props => <Environment {...props} store={store} gimletClient={gimletClient} />);
    const EnvConfigWithRouting = withRouter(props => <EnvConfig {...props} store={store} gimletClient={gimletClient} />);
    const DeployWizzardWithRouting = withRouter(props => <DeployWizzard {...props} store={store} gimletClient={gimletClient} />);
    const RepositoryWizardWithRouting = withRouter(props => <RepositoryWizard {...props} store={store} gimletClient={gimletClient} />);
    const GithubIntegrationWithRouting = withRouter(props => <GithubIntegration {...props} store={store} gimletClient={gimletClient} />);
    const PopUpWindowWithLocation = withRouter(props => <PopUpWindow {...props} store={store} />);
    const ProfileWithRouting = withRouter(props => <Profile {...props} store={store} gimletClient={gimletClient} />);
    const SettingsWithRouting = withRouter(props => <Settings {...props} store={store} gimletClient={gimletClient} />);
    const FooterWithRouting = withRouter(props => <Footer {...props} store={store} gimletClient={gimletClient} />)
    const CommitViewWithRouting = withRouter(props => <CommitView {...props} store={store} gimletClient={gimletClient} />)
    const RepoSettingsViewWithRouting = withRouter(props => <RepoSettingsView {...props} store={store} gimletClient={gimletClient} />)
    const PreviewViewWithRouting = withRouter(props => <PreviewView {...props} store={store} gimletClient={gimletClient} />)

    if (!this.state.userLoaded) {
      return (<div>loading</div>)
    }

    if (!this.state.authenticated) {
      return (
        <Router>
          <div className="min-h-screen bg-neutral-100 dark:bg-neutral-900 pb-20">
            <div className="py-10">
              <Switch>
                <Route path="/login">
                  <LoginPage />
                </Route>
              </Switch>
            </div>
          </div>
        </Router>
      )
    }

    if (!this.state.settings) {
      return (<div>loading</div>)
    }

    if(!this.state.settings.provider || this.state.settings.provider === ""){
      return (
        <Router>
          <div className="min-h-screen bg-neutral-100 dark:bg-neutral-900 pb-20">
            <GithubIntegrationWithRouting />
          </div>
        </Router>
      )
    }

    return (
      <Router>
        <StreamingBackendWithLocation />
        <APIBackendWithLocation />
        <PopUpWindowWithLocation />
        <Posthog store={store} />

        <Route exact path="/">
          <Redirect to="/repositories" />
        </Route>

        <div className="min-h-screen bg-neutral-100 dark:bg-neutral-900 pb-20">
          <FooterWithRouting />
          <div className="">
            <Switch>
              <Route path="/repositories">
                <NavBar />
                <RepositoriesWithRouting />
              </Route>

              <Route path="/environments">
                <NavBar />
                <EnvironmentsWithRouting />
              </Route>

              <Route path="/env/:env/:tab?">
                <NavBar />
                <EnvironmentWithRouting />
              </Route>

              <Route path="/cli">
                <NavBar />
                <ProfileWithRouting store={store} />
              </Route>

              <Route path="/settings">
                <NavBar />
                <SettingsWithRouting store={store} />
              </Route>

              <Route path="/login">
                <NavBar />
                <LoginPage />
              </Route>

              <Route path="/repo/:owner/:repo/envs/:env/config/:config/:action?/:nav?">
                <NavBar />
                <EnvConfigWithRouting />
              </Route>

              <Route path="/repo/:owner/:repo/envs/:env/deploy">
                <NavBar />
                <DeployWizzardWithRouting />
              </Route>

              <Route path="/import-repositories">
                <NavBar />
                <RepositoryWizardWithRouting />
              </Route>

              <Route path="/repo/:owner/:repo/commits">
                <NavBar />
                <CommitViewWithRouting />
              </Route>

              <Route path="/repo/:owner/:repo/settings/:nav?">
                <NavBar />
                <RepoSettingsViewWithRouting />
              </Route>

              <Route path="/repo/:owner/:repo/previews/:deployment?">
                <NavBar />
                <PreviewViewWithRouting />
              </Route>

              <Route path="/repo/:owner/:repo/:environment?/:deployment?">
                <NavBar />
                <RepoWithRouting />
              </Route>

            </Switch>
          </div>
        </div>
      </Router>
    )
  }
}
