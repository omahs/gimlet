const BootstrapGuide = ({ envName, repoPath, repoPerEnv, publicKey, secretFileName, gitopsRepoFileName, isNewRepo }) => {
    const repoName = parseRepoName(repoPath);
    let type = "";

    if (repoPath.includes("apps")) {
        type = "apps";
    } else if (repoPath.includes("infra")) {
        type = "infra";
    }

    const renderBootstrapGuideText = (isNewRepo) => {
        return isNewRepo ? (
            <>
                <li>👉 Clone the Gitops repository</li>
                <li className="text-xs font-mono bg-blue-100 font-medium text-blue-500 px-1 py-1 rounded">git clone git@github.com:{repoPath}.git</li>
                <li>👉 Add the following deploy key to your Git provider</li>
                <li className="text-xs font-mono bg-blue-100 font-medium text-blue-500 px-1 py-1 rounded">{publicKey}</li>
                <li>👉 Apply the gitops manifests on the cluster to start the gitops loop:</li>
                <ul className="list-none text-xs font-mono bg-blue-100 font-medium text-blue-500 px-1 py-1 rounded">
                    <li>{repoPerEnv ? `kubectl apply -f ${repoName}/flux/flux.yaml` : `kubectl apply -f ${repoName}/${envName}/flux/flux.yaml`}</li>
                    <li>{repoPerEnv ? `kubectl apply -f ${repoName}/flux/${secretFileName}` : `kubectl apply -f ${repoName}/${envName}/flux/${secretFileName}`}</li>
                    <li>kubectl wait --for condition=established --timeout=60s crd/gitrepositories.source.toolkit.fluxcd.io</li>
                    <li>kubectl wait --for condition=established --timeout=60s crd/kustomizations.kustomize.toolkit.fluxcd.io</li>
                    <li>{repoPerEnv ? `kubectl apply -f ${repoName}/flux/${gitopsRepoFileName}` : `kubectl apply -f ${repoName}/${envName}/flux/${gitopsRepoFileName}`}</li>
                </ul>
                <li>Happy Gitopsing🎊</li>
            </>
        ) : (
            <>
                <li>👉 Clone the Gitops repository</li>
                <li className="text-xs font-mono bg-blue-100 font-medium text-blue-500 px-1 py-1 rounded">git clone git@github.com:{repoPath}.git</li>
                <li>👉 Apply the gitops manifests on the cluster to start the gitops loop:</li>
                <ul className="list-none text-xs font-mono bg-blue-100 font-medium text-blue-500 px-1 py-1 rounded">
                    <li>{repoPerEnv ? `kubectl apply -f ${repoName}/flux/${gitopsRepoFileName}` : `kubectl apply -f ${repoName}/${envName}/flux/${gitopsRepoFileName}`}</li>
                </ul>
                <li>Happy Gitopsing🎊</li>
            </>
        )

    };

    return (
        <div className="rounded-md bg-blue-50 p-4 mb-4 overflow-hidden">
            <ul className="break-all text-sm text-blue-700 space-y-2">
                <span className="text-lg font-bold text-blue-800">Gitops {type}</span>
                {renderBootstrapGuideText(isNewRepo)}
            </ul>
        </div>
    );
};

const parseRepoName = (repo) => {
    return repo.split("/")[1];
};

export default BootstrapGuide;