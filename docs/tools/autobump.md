# autobump

## 设计背景

在维护 TiChi 的过程当中，我们需要经常更新上游的 Prow 及其相关的组件和插件，需要在本地手动地修改相关文件中的版本号，然后以 Pull Request 的方式将更新提交到 master 分支，触发自动部署脚本启用新版本的组件和插件。

如果能够通过脚本的方式自动地完成更新依赖这个步骤，将在一定程度上提高维护的效率。

因此，我们可以使用 Kubernetes 社区设计开发的 [`autobump`](https://github.com/kubernetes/test-infra/tree/master/prow/cmd/autobump) 工具来自动提交更新依赖版本号的 Pull Request。

## 设计思路

Kubernetes 社区的 `autobump` 工具被打包在 Docker 镜像当中，其中包含了两个脚本：

- [`bump.sh`](https://github.com/kubernetes/test-infra/blob/master/prow/cmd/autobump/bump.sh) 脚本会将相关文件当中组件或插件的镜像版本号更新至上游版本、最新版本或特定版本。
  
- [`autobump.sh`](https://github.com/kubernetes/test-infra/blob/master/prow/cmd/autobump/autobump.sh) 会通过 `bump.sh` 脚本完成版本号的更新，然后将修改好的配置文件推送到从待更新仓库 fork 出来的 Github 仓库，最后使用 [`pr-creator`](https://github.com/kubernetes/test-infra/tree/master/robots/pr-creator) 来创建一个 Pull Request。

如果要实现定期的自动更新，我们可以定义一个 `periodic` 类型的 ProwJob 来定期执行 `autobump` 脚本，具体配置可以参考下文的 “配置说明”。

通常情况下，我们还可以使用一个 `postsubmit` 类型的 ProwJob 在版本号更新的 PR 被合并之后进行自动部署。

配置完成之后，维护者就只需要通过合并 PR 的方式完成 Prow 版本的自动更新。

## 配置说明

### 准备工作

- 准备一个拥有 `repo` 范围权限的 Github 账号的 [Access Token](https://github.com/settings/tokens) ，`autobump.sh` 将会使用该 Github 账号来提交 Pull Request。
  
- 必须事先将待更新的 Github 仓库 fork 到提交 PR 所用的 Github 账号，如果 Fork 出来的仓库与源仓库名称不一致，需要通过环境变量 `FORK_GH_REPO` 指定。

- 如果容器当中没有自动提供 GCP 凭据，就必须提供 [JSON 格式的服务账号密钥文件](https://cloud.google.com/iam/docs/creating-managing-service-account-keys) 并通过环境变量 `GOOGLE_APPLICATION_CREDENTIALS` 来指定该文件的路径。如果你使用的是 `gcr.io/k8s-prow/*` 上的镜像则可以忽略该步骤，因为它是公开可读的。
  
### 配置示例

```yaml
periodics:
  - name: periodic-tichi-autobump
    # 在工作日的 15 时到 23 时范围内，每个小时的第 5 分钟进行以此自动更新的检查。
    cron: "05 7-15 * * 1-5"
    decorate: true
    extra_refs:
      # 包含 Prow 实例的配置文件和部署文件的仓库（也就是需要进行自动更新版本的仓库）
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
            # 提交 PR 的 Github 账号的名称和电子邮箱
            - "ti-chi-bot"
            - ti-community-prow-bot@tidb.io
          volumeMounts:
            - name: github-token
              mountPath: /etc/github
              readOnly: true
          env:
            # autobump.sh 的参数
            # 待更新仓库的所属组织名称
            - name: GH_ORG
              value: ti-community-infra
            # 待更新仓库的名称
            - name: GH_REPO
              value: tichi
            # plank 组件部署文件的相对路径，autobump 将该组件的镜像版本视为当前版本（旧的版本）
            - name: PROW_CONTROLLER_MANAGER_FILE
              value: configs/prow-dev/cluster/prow_controller_manager_deployment.yaml
            # bump.sh 的参数
            # Prow 组件的 K8S 部署文件所在目录（使用逗号区分多个目录）
            - name: COMPONENT_FILE_DIR
              value: configs/prow-dev/cluster
              # value: config/prow/cluster,prow/config/jobs
            # Prow 核心配置文件（config.yaml）的相对路径
            - name: CONFIG_PATH
              value: configs/prow-dev/config/config.yaml
            # 仓库当中 ProwJob 配置文件的路径或配置文件所在目录的路径，如果 ProwJob 的配置定义在 config.yaml 文件当中则可忽略该配置
            - name: JOB_CONFIG_PATH
              value: "./"
      volumes:
        - name: github-token
          secret:
            # 拥有 repo 范围权限的 Github 用户的 Access Token
            secretName: github-token
```

### 测试

你可以在添加 `periodics` 类型 ProwJob 之前，先提交一个使用 `presubmit` 类型的 ProwJob 的 Pull Request 对以上配置进行测试，测试的时候不需要指定 `cron` 选项，并且需要并将 `always_run` 选项设置为 `true`。

## 参考文档

- [README](https://github.com/kubernetes/test-infra/blob/master/prow/cmd/autobump/README.md)
- [代码实现](https://github.com/kubernetes/test-infra/tree/master/prow/cmd/autobump)
- [官方示例](https://github.com/kubernetes/test-infra/blob/master/prow/cmd/autobump/example-periodic.yaml)
