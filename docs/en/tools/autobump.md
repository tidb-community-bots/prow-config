# autobump

## Design Background

In the process of maintaining TiChi, we need to frequently update the upstream Prow and its related components and plugins. We need to manually modify the version number in the relevant file locally, and then submit the update to the master branch by Pull Request to trigger Automatic deployment scripts enable new versions of components and plugins. 

If the update dependency step can be automatically completed through scripts, the maintenance efficiency will be improved to a certain extent.

Therefore, we can use the [`autobump`](https://github.com/kubernetes/test-infra/tree/master/prow/cmd/autobump) tool designed and developed by the Kubernetes community to automatically submit a Pull Request that updates the dependent version number.
## Design

The `autobump` tool of the Kubernetes community is packaged in the Docker image, which contains two scripts:

- The [`bump.sh`](https://github.com/kubernetes/test-infra/blob/master/prow/cmd/autobump/bump.sh) script will update the mirror version number of the component or plug-in in the relevant file to the upstream version, the latest version or a specific version.

- The [`autobump.sh`](https://github.com/kubernetes/test-infra/blob/master/prow/cmd/autobump/autobump.sh) script will update the version number through the `bump.sh` script, then push the modified configuration file to the Github repository forked from the repository to be updated, and finally use [`pr-creator`](https://github.com/kubernetes/test-infra/tree/master/robots/pr-creator) to create a Pull Request.

If you want to implement regular automatic updates, we can define a `periodic` type ProwJob to periodically execute the `autobump` script, the specific configuration can refer to the "Configuration Instruction" below.

Usually, we can also use a `postsubmit` type ProwJob to automatically deploy after the version number updated PR is merged.

After the configuration is complete, the maintainer only needs to complete the automatic update of the Prow version by merging the PR.

## Configuration Instruction

### Preparation

- Prepare a [Access Token](https://github.com/settings/tokens) of a Github account with the scope of `repo` permission, `autobump.sh` will use this Github account to submit a pull request.

- The Github repository to be updated must be forked in advance to the Github account used to submit the PR. If the name of the Fork repository is inconsistent with the source repository, it needs to be specified by the environment variable `FORK_GH_REPO`.

- If the GCP credentials are not automatically provided in the container, you must provide a [service account key file in JSON format](https://cloud.google.com/iam/docs/creating-managing-service-account-keys) and pass The environment variable `GOOGLE_APPLICATION_CREDENTIALS` is used to specify the path of the file. If you are using the mirror on `gcr.io/k8s-prow/*`, you can ignore this step because it is publicly readable.

### Configuration Example

```yaml
periodics:
  - name: periodic-tichi-autobump
    # In the range of 15:00 to 23:00 on weekdays, the automatic update check is performed at the 5th minute of each hour. (In UTC+8 timezone)
    cron: "05 7-15 * * 1-5"
    decorate: true
    extra_refs:
      # The repository containing the configuration files and deployment files of the Prow instance (The repository that needs to be automatically updated).
      - org: ti-community-infra
        repo: tichi
        base_ref: master
    spec:
      containers:
        - image: gcr.io/k8s-prow/autobump:v20210818-b81cb0bc7b
          command:
            - /autobump.sh
          args:
            - /etc/github/token
            # The name and email address of the Github account that submitted the PR.
            - "ti-chi-bot"
            - ti-community-prow-bot@tidb.io
          volumeMounts:
            - name: github-token
              mountPath: /etc/github
              readOnly: true
          env:
            # autobump.sh args:
            # Organization name of the repository to be updated.
            - name: GH_ORG
              value: ti-community-infra
            # The name of the repository to be updated.
            - name: GH_REPO
              value: tichi
            # The relative path of the plank component deployment file, 
            # autobump regards the image version of the component as the current version (old version).
            - name: PROW_CONTROLLER_MANAGER_FILE
              value: configs/prow-dev/cluster/prow_controller_manager_deployment.yaml
            # bump.sh args:
            # The directory where the K8S deployment files of the Prow component are located,
            # use a comma to distinguish multiple directories.
            - name: COMPONENT_FILE_DIR
              value: configs/prow-dev/cluster
              # value: config/prow/cluster,prow/config/jobs
            # The relative path of the Prow core configuration file (config.yaml).
            - name: CONFIG_PATH
              value: configs/prow-dev/config/config.yaml
            # The path of the ProwJob configuration file in the repository or the path of the directory 
            # where the configuration file is located. 
            # If the configuration of ProwJob is defined in the config.yaml file, the configuration can be ignored.
            - name: JOB_CONFIG_PATH
              value: "./"
      volumes:
        - name: github-token
          secret:
            # Access Token of a Github user with repo scope permissions.
            secretName: github-token
```
### Test

You can submit a pull request of a ProwJob of type `presubmit` to test the above configuration before adding a ProwJob of type `periodics`. When testing, you do not need to specify the `cron` option, and you need to set the `always_run` option to `true`.

## Reference Documents

- [README](https://github.com/kubernetes/test-infra/blob/master/prow/cmd/autobump/README.md)
- [code](https://github.com/kubernetes/test-infra/tree/master/prow/cmd/autobump)
- [official configuration example](https://github.com/kubernetes/test-infra/blob/master/prow/cmd/autobump/example-periodic.yaml)