import ServiceDetail from "../serviceDetail/serviceDetail";
import { InformationCircleIcon } from '@heroicons/react/20/solid';

export function Env(props) {
  const { store, gimletClient } = props
  const { env, repoRolloutHistory, envConfigs, navigateToConfigEdit, linkToDeployment, rollback, owner, repoName, fileInfos } = props;
  const { releaseHistorySinceDays, deploymentFromParams, scmUrl, history, alerts, appFilter } = props;

  const renderedServices = renderServices(env.stacks, envConfigs, env, repoRolloutHistory, navigateToConfigEdit, linkToDeployment, rollback, owner, repoName, fileInfos, releaseHistorySinceDays, gimletClient, store, deploymentFromParams, scmUrl, alerts, appFilter);

  return (
    <div>
      <h4 className="relative flex items-stretch select-none text-xl font-medium capitalize leading-tight text-neutral-900 dark:text-neutral-200 my-4">
        {env.name}
        <span title={env.isOnline ? "Connected" : "Disconnected"}>
          <svg className={(env.isOnline ? "text-green-400 dark:text-teal-600" : "text-red-400") + " inline fill-current ml-1"} xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 20 20">
            <path
              d="M0 14v1.498c0 .277.225.502.502.502h.997A.502.502 0 0 0 2 15.498V14c0-.959.801-2.273 2-2.779V9.116C1.684 9.652 0 11.97 0 14zm12.065-9.299l-2.53 1.898c-.347.26-.769.401-1.203.401H6.005C5.45 7 5 7.45 5 8.005v3.991C5 12.55 5.45 13 6.005 13h2.327c.434 0 .856.141 1.203.401l2.531 1.898a3.502 3.502 0 0 0 2.102.701H16V4h-1.832c-.758 0-1.496.246-2.103.701zM17 6v2h3V6h-3zm0 8h3v-2h-3v2z"
            />
          </svg>
        </span>
      </h4>
      <div className="space-y-4">
        {!env.isOnline &&
          <ConnectEnvCard history={history} envName={env.name}/>
        }
        {renderedServices.length === 10 &&
          <span className="text-xs text-blue-700">Displaying at most 10 application configurations per environment.</span>
        }
        {renderedServices.length !== 0 &&
          <>
            {renderedServices}
          </>
        }
        { renderedServices.length === 0 && emptyStateDeployThisRepo(history,env.name, owner, repoName) }
      </div>
    </div>
  )
}

function renderServices(
  stacks,
  envConfigs,
  environment,
  repoRolloutHistory,
  navigateToConfigEdit,
  linkToDeployment,
  rollback,
  owner,
  repoName,
  fileInfos,
  releaseHistorySinceDays,
  gimletClient,
  store,
  deploymentFromParams,
  scmUrl,
  alerts,
  appFilter) {
  let services = [];

  let configsWeHave = [];
  if (envConfigs) {
    configsWeHave = envConfigs
      .filter((config) => !config.preview)
      .map((config) => config.app);
  }

  const filteredStacks = stacks
    .filter(stack => stack.service.name.includes(appFilter)) // app filter
    .filter(stack => stack.deployment?.branch === "") // filter preview deploys from this view

  let configsWeDeployed = [];
  // render services that are deployed on k8s
  services = filteredStacks.map((stack) => {
    configsWeDeployed.push(stack.service.name);
    const configExists = configsWeHave.includes(stack.service.name)
    let config = undefined;
    if (configExists) {
      config = envConfigs.find((config) => config.app === stack.service.name)
    }

    let deployment = "";
    if (stack.deployment) {
      deployment = stack.deployment.namespace + "/" + stack.deployment.name
    }

    return (
      <div key={'sc-'+stack.service.name} className="w-full flex items-center justify-between space-x-6 p-4 card">
        <ServiceDetail
          key={'sc-'+stack.service.name}
          stack={stack}
          rolloutHistory={repoRolloutHistory?.[environment.name]?.[stack.service.name]}
          rollback={rollback}
          environment={environment}
          owner={owner}
          repoName={repoName}
          fileName={fileName(fileInfos, stack.service.name)}
          navigateToConfigEdit={navigateToConfigEdit}
          linkToDeployment={linkToDeployment}
          configExists={configExists}
          config={config}
          releaseHistorySinceDays={releaseHistorySinceDays}
          gimletClient={gimletClient}
          store={store}
          deploymentFromParams={deploymentFromParams}
          scmUrl={scmUrl}
          serviceAlerts={alerts[deployment]}
        />
      </div>
    )
  })

  if (services.length >= 10) {
    return services.slice(0, 10);
  }

  const configsWeHaventDeployed = configsWeHave.filter(config => !configsWeDeployed.includes(config) && config.includes(appFilter));

  services.push(
    ...configsWeHaventDeployed.sort().map(config => {
      return (
        <div key={config} className="w-full flex items-center justify-between space-x-6 p-4 pb-8 card">
          <ServiceDetail
            key={config}
            stack={{
              service: {
                name: config
              }
            }}
            rolloutHistory={repoRolloutHistory?.[environment.name]?.[config]}
            rollback={rollback}
            environment={environment}
            owner={owner}
            repoName={repoName}
            fileName={fileName(fileInfos, config)}
            navigateToConfigEdit={navigateToConfigEdit}
            linkToDeployment={linkToDeployment}
            configExists={true}
            releaseHistorySinceDays={releaseHistorySinceDays}
            gimletClient={gimletClient}
            store={store} 
            deploymentFromParams={deploymentFromParams}
            scmUrl={scmUrl}
          />
        </div>)
    }
    )
  )
  return services.slice(0, 10)
}

function fileName(fileInfos, appName) {
  if (fileInfos.find(fileInfo => fileInfo.appName === appName)) {
    return fileInfos.find(fileInfo => fileInfo.appName === appName).fileName;
  }
}

function ConnectEnvCard(props) {
  const {history, envName} = props

  return (
    <div className="rounded-md bg-red-200 p-4">
    <div className="flex">
      <div className="flex-shrink-0">
        <InformationCircleIcon className="h-5 w-5 text-red-400" aria-hidden="true" />
      </div>
      <div className="ml-3">
        <h3 className="text-sm font-bold text-red-800">Environment disconnected</h3>
        <div className="mt-2 text-sm text-red-800">
          This environment is disconnected.<br />
          <button className='font-bold cursor-pointer'
            onClick={() => {history.push(`/env/${envName}`);return true}}
          >
            Click to connect this environment to a cluster on the Environments page.
          </button>
        </div>
      </div>
    </div>
    </div>
  );
}

function emptyStateDeployThisRepo(history, envName, owner, repo) {
  return (
    <div className='card w-full p-4 mt-4'>
      <div className='items-center border-dashed border border-neutral-200 dark:border-neutral-700 rounded-md p-4 py-16'>
        <h3 className="mt-2 text-sm font-semibold text-center">No Deployments</h3>
        <p className="mt-1 text-sm text-neutral-500 dark:text-neutral-400 text-center">Get started by configuring a new deployment.</p>
        <div className="mt-6 text-center">
          <button
            onClick={() => history.push(encodeURI(`/repo/${owner}/${repo}/envs/${envName}/deploy`))}
            className="primaryButton px-8 capitalize">
            New Deployment to {envName}
          </button>
        </div>
      </div>
    </div>
  )
}
