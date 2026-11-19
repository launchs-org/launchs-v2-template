package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"

	"github.com/google/uuid"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"launchs/shared/model"
	"launchs/shared/template"
	"launchs/shared/templateplan"
	"launchs/worker/src/activity"
	"launchs/worker/src/cluster"
)

// specSizes は、deploy の spec の、CPU(ミリコア)とメモリ(MiB)。本体の migrations/app/000003_create_deploys.up.sql の値と同じ。
var specSizes = map[string][2]int{
	"small":  {250, 256},
	"medium": {500, 512},
	"large":  {1000, 1024},
}

// runnerSettings は、展開の設定。本番の worker の環境変数に当たる。
type runnerSettings struct {
	// StorageClass は、PVC の StorageClass(空なら、クラスターの既定)。
	StorageClass string
	// AccessMode は、PVC のアクセスモード。
	AccessMode string
	// IngressNamespace は、外からの通信を中継する namespace(NetworkPolicy で許可する)。
	IngressNamespace string
	// TraefikAPIVersion は、IngressRoute の apiVersion。
	TraefikAPIVersion string
	// EntryPoints は、IngressRoute の entryPoint。
	EntryPoints []string
	// SkipIngress は、IngressRoute を作らない(CRD が無いクラスター用)。
	SkipIngress bool
}

// runner は、計画を、本番と同じ k8s の operator で、クラスターに展開する。
type runner struct {
	settings      runnerSettings
	deployments   *cluster.KubernetesDeploymentOperator
	volumes       *cluster.KubernetesVolumeOperator
	configMaps    *cluster.KubernetesConfigMapOperator
	configMapRevs *cluster.KubernetesConfigMapRevisionOperator
	services      *cluster.KubernetesServiceOperator
	ingresses     *cluster.KubernetesIngressOperator
}

// newRunner は、clientset と dynamic client から、本番と同じ operator を組み立てる。
//
// 引数:
//   - clientset: k8s のクライアント(本物でも、偽物でもよい)。
//   - dynamicClient: IngressRoute 用のクライアント。
//   - settings: 展開の設定。
//
// 戻り値:
//   - runner と、ingress の operator を作れないときのエラー。
func newRunner(clientset kubernetes.Interface, dynamicClient dynamic.Interface, settings runnerSettings) (*runner, error) {
	// ingress の operator を作る。
	ingressOperator, ingressError := cluster.NewKubernetesIngressOperator(dynamicClient, cluster.IngressRouteSettings{APIVersion: settings.TraefikAPIVersion, EntryPoints: settings.EntryPoints})
	if ingressError != nil {
		return nil, ingressError
	}
	return &runner{
		settings:      settings,
		deployments:   cluster.NewKubernetesDeploymentOperator(clientset, cluster.OperatorSettings{IngressNamespace: settings.IngressNamespace}),
		volumes:       cluster.NewKubernetesVolumeOperator(clientset, cluster.VolumeSettings{StorageClass: settings.StorageClass, AccessMode: settings.AccessMode}),
		configMaps:    cluster.NewKubernetesConfigMapOperator(clientset),
		configMapRevs: cluster.NewKubernetesConfigMapRevisionOperator(clientset),
		services:      cluster.NewKubernetesServiceOperator(clientset),
		ingresses:     ingressOperator,
	}, nil
}

// newProjectNamespace は、本番のプロジェクトと同じ形(project- + UUID)の namespace 名を作る。
//
// 戻り値:
//   - namespace の名前。
func newProjectNamespace() string {
	// 接頭辞に、UUID v4 を付ける。
	return model.ProjectIDPrefix + uuid.NewString()
}

// buildPlan は、テンプレートと変数の値から、本番と同じ関数で、計画を作る。
//
// 引数:
//   - target: テンプレート。
//   - overrides: 利用者が指定した変数の値(指定しない変数は、既定値や、生成した値)。
//   - projectName: ${project.name} になる名前。
//
// 戻り値:
//   - 計画と、解決した変数の値。
//   - 変数の値が不正なとき、展開できないときのエラー。
func buildPlan(target template.Template, overrides map[string]string, projectName string) (templateplan.Plan, map[string]string, error) {
	// 変数の値を決める(生成するパスワードも、ここで作られる)。
	values, valuesError := template.ResolveValues(target, overrides)
	if valuesError != nil {
		return templateplan.Plan{}, nil, valuesError
	}
	// 計画を作る。ホスト名は、試験用の名前にする。
	plan, planError := template.BuildPlan(target, values, template.PlanOptions{
		ProjectName: projectName,
		DNSToken:    uuid.NewString()[:8],
		Hosts:       []string{target.Metadata.ID + ".harness.localhost"},
		NewID:       func(prefix string) string { return prefix + uuid.NewString() },
	})
	return plan, values, planError
}

// apply は、計画の全てのステップを、順に、クラスターに展開する。
//
// 処理の流れ(本番の ApplyTemplateWorkflow と、同じ順序):
//  1. namespace を作る(NetworkPolicy も付く)。
//  2. ステップを、計画の順に、volume → configmap → service と deploy → ingress と作る。
//  3. deploy は、先に作った service の DNS 名で、環境変数・引数・ファイルの目印を置き換える。
//
// 引数:
//   - requestContext: 処理の context。
//   - plan: 計画。
//   - namespaceName: 展開先の namespace。
//
// 戻り値:
//   - 作った ingress の、ルールの一覧(smoke の確認に使う)。
//   - 失敗したときのエラー(どのステップかを含む)。
func (target *runner) apply(requestContext context.Context, plan templateplan.Plan, namespaceName string) ([]activity.IngressRouteDefinition, error) {
	// namespace を作る。
	if namespaceError := target.deployments.EnsureNamespace(requestContext, namespaceName); namespaceError != nil {
		return nil, namespaceError
	}
	// deploy 名 → DNS 名。service を作るたびに増える。
	dnsNames := map[string]string{}
	created := []activity.IngressRouteDefinition{}
	for _, step := range plan.Steps {
		switch step.Kind {
		case templateplan.StepVolume:
			// PVC を作る。
			if volumeError := target.volumes.EnsureVolumeClaim(requestContext, activity.VolumeClaimDefinition{Namespace: namespaceName, Name: step.ID, SizeMB: step.Volume.SizeMB}); volumeError != nil {
				return nil, fmt.Errorf("volume %s: %w", step.Name, volumeError)
			}
		case templateplan.StepConfigMap:
			// 共有 ConfigMap の、第 1 版を作る。
			if configMapError := target.applyConfigMap(requestContext, namespaceName, step); configMapError != nil {
				return nil, fmt.Errorf("configmap %s: %w", step.Name, configMapError)
			}
		case templateplan.StepService:
			// Service を作り、DNS 名を、本番と同じ形で決める。
			dnsName, serviceError := target.applyService(requestContext, namespaceName, plan, step)
			if serviceError != nil {
				return nil, fmt.Errorf("service %s: %w", step.Name, serviceError)
			}
			dnsNames[step.Service.Deploy] = dnsName
		case templateplan.StepDeploy:
			// deploy を作る。
			if deployError := target.applyDeploy(requestContext, namespaceName, plan, step, dnsNames); deployError != nil {
				return nil, fmt.Errorf("deploy %s: %w", step.Name, deployError)
			}
		case templateplan.StepIngress:
			// CRD が無いクラスターでは、作らない。
			if target.settings.SkipIngress {
				continue
			}
			definition, ingressError := target.applyIngress(requestContext, namespaceName, plan, step)
			if ingressError != nil {
				return nil, fmt.Errorf("ingress: %w", ingressError)
			}
			created = append(created, definition)
		}
	}
	return created, nil
}

// applyConfigMap は、共有 ConfigMap の第 1 版を作る(本番と同じ、版つきの名前)。
//
// 引数:
//   - requestContext: 処理の context。
//   - namespaceName: 展開先の namespace。
//   - step: configmap のステップ。
//
// 戻り値:
//   - 作れないときのエラー。
func (target *runner) applyConfigMap(requestContext context.Context, namespaceName string, step templateplan.Step) error {
	// text と base64 に分ける。
	data := map[string]string{}
	binaryData := map[string][]byte{}
	for _, file := range step.ConfigMap.Files {
		// base64 は、デコードして、バイナリにする。
		if file.Encoding == model.ConfigMapEncodingBase64 {
			decoded, decodeError := base64.StdEncoding.DecodeString(file.Content)
			if decodeError != nil {
				return decodeError
			}
			binaryData[file.Key] = decoded
			continue
		}
		data[file.Key] = file.Content
	}
	// 版つきの名前で、作る。
	return target.configMapRevs.ApplyRevisionConfigMap(requestContext, namespaceName, model.ConfigMapRevisionResourceName(step.ID, 1), data, binaryData)
}

// applyService は、k8s の Service を作り、DNS 名を返す。
//
// 引数:
//   - requestContext: 処理の context。
//   - namespaceName: 展開先の namespace。
//   - plan: 計画(deploy の ID の引き当てに使う)。
//   - step: service のステップ。
//
// 戻り値:
//   - DNS 名(<service の ID>.<namespace>.svc.cluster.local)。
//   - 作れないときのエラー。
func (target *runner) applyService(requestContext context.Context, namespaceName string, plan templateplan.Plan, step templateplan.Step) (string, error) {
	// ポートを、定義にする。
	ports := make([]activity.ServicePortDefinition, 0, len(step.Service.Ports))
	for _, port := range step.Service.Ports {
		ports = append(ports, activity.ServicePortDefinition{Name: port.Name, Port: int32(port.Port), Protocol: port.Protocol})
	}
	// Service を作る。向き先の deploy の ID は、計画に入っている。
	definition := activity.ServiceDefinition{Namespace: namespaceName, Name: step.ID, DeployID: plan.IDOf(templateplan.StepDeploy, step.Service.Deploy), Type: "ClusterIP", Ports: ports}
	if _, applyError := target.services.ApplyService(requestContext, definition); applyError != nil {
		return "", applyError
	}
	// 本番と同じ形の DNS 名。
	return fmt.Sprintf("%s.%s.svc.cluster.local", step.ID, namespaceName), nil
}

// applyDeploy は、deploy を、本番と同じ定義にして、Deployment として作る。
//
// 引数:
//   - requestContext: 処理の context。
//   - namespaceName: 展開先の namespace。
//   - plan: 計画(ID の引き当てに使う)。
//   - step: deploy のステップ。
//   - dnsNames: これまでに決まった、deploy 名 → DNS 名。
//
// 戻り値:
//   - 作れないとき、DNS 名が決まっていないときのエラー。
func (target *runner) applyDeploy(requestContext context.Context, namespaceName string, plan templateplan.Plan, step templateplan.Step, dnsNames map[string]string) error {
	deployPlan := step.Deploy
	// 目印を、DNS 名に置き換える関数。
	replace := func(text string) string { return templateplan.ReplaceDNS(text, plan.DNSToken, dnsNames) }
	// 環境変数、引数、ファイルを置き換える。
	environment := map[string]string{}
	for name, value := range deployPlan.Env {
		environment[name] = replace(value)
	}
	arguments := make([]string, 0, len(deployPlan.Args))
	for _, argument := range deployPlan.Args {
		arguments = append(arguments, replace(argument))
	}
	files := make([]model.FileSpec, 0, len(deployPlan.Files))
	for _, file := range deployPlan.Files {
		files = append(files, model.FileSpec{Path: file.Path, Content: replace(file.Content)})
	}
	// 決まっていない目印が残っていたら、失敗にする(本番と同じ)。
	remaining := []string{replace(deployPlan.Command)}
	remaining = append(remaining, arguments...)
	for _, value := range environment {
		remaining = append(remaining, value)
	}
	for _, file := range files {
		remaining = append(remaining, file.Content)
	}
	for _, value := range remaining {
		if templateplan.HasUnresolvedDNS(value, plan.DNSToken) {
			return fmt.Errorf("service の DNS 名が、まだ決まっていません(参照する service を、先に作る必要があります)")
		}
	}
	// spec の大きさを引く。
	size, knownSpec := specSizes[deployPlan.Spec]
	if !knownSpec {
		return fmt.Errorf("知らない spec です: %s", deployPlan.Spec)
	}
	// ファイルは、パスの昇順に f0、f1… の key で、deploy 専用の ConfigMap に入れる(本番と同じ)。
	sort.Slice(files, func(first int, second int) bool { return files[first].Path < files[second].Path })
	fileDefinitions := make([]activity.FileDefinition, 0, len(files))
	configMapData := map[string]string{}
	for index, file := range files {
		key := "f" + strconv.Itoa(index)
		fileDefinitions = append(fileDefinitions, activity.FileDefinition{Key: key, Path: file.Path})
		configMapData[key] = file.Content
	}
	if len(files) > 0 {
		if configMapError := target.configMaps.ApplyConfigMap(requestContext, namespaceName, step.ID, configMapData); configMapError != nil {
			return configMapError
		}
	}
	// マウントを定義にする。
	mounts := make([]activity.MountDefinition, 0, len(deployPlan.Mounts))
	for _, mount := range deployPlan.Mounts {
		mounts = append(mounts, activity.MountDefinition{ClaimName: plan.IDOf(templateplan.StepVolume, mount.Volume), MountPath: mount.Path})
	}
	configMapMounts := make([]activity.ConfigMapMountDefinition, 0, len(deployPlan.ConfigMaps))
	for _, mount := range deployPlan.ConfigMaps {
		spec := model.ConfigMapMountSpec{ConfigMapID: plan.IDOf(templateplan.StepConfigMap, mount.ConfigMap), Revision: 1, Key: mount.Key, Path: mount.Path, Mode: mount.Mode}
		configMapMounts = append(configMapMounts, activity.ConfigMapMountDefinition{ConfigMapName: model.ConfigMapRevisionResourceName(spec.ConfigMapID, 1), Key: mount.Key, Path: mount.Path, Mode: spec.ModeBits()})
	}
	// 定義を組み立てる。
	definition := activity.DeploymentDefinition{
		Namespace: namespaceName, Name: step.ID, Image: replace(deployPlan.Image), Replicas: int32(deployPlan.Replicas),
		CPUMillicores: size[0], MemoryMiB: size[1], Env: environment, Args: arguments,
		Mounts: mounts, Files: fileDefinitions, ConfigMapMounts: configMapMounts,
	}
	if command := replace(deployPlan.Command); command != "" {
		definition.Command = &command
	}
	if deployPlan.FSGroup > 0 {
		group := int64(deployPlan.FSGroup)
		definition.FSGroup = &group
	}
	// Deployment を作る。
	_, applyError := target.deployments.ApplyDeployment(requestContext, definition)
	return applyError
}

// applyIngress は、IngressRoute を作る。
//
// 引数:
//   - requestContext: 処理の context。
//   - namespaceName: 展開先の namespace。
//   - plan: 計画(service の ID の引き当てに使う)。
//   - step: ingress のステップ。
//
// 戻り値:
//   - 作った定義(smoke の確認に使う)。
//   - 作れないときのエラー。
func (target *runner) applyIngress(requestContext context.Context, namespaceName string, plan templateplan.Plan, step templateplan.Step) (activity.IngressRouteDefinition, error) {
	// ルールを、service の ID 向きに直す。
	rules := make([]activity.IngressRouteRule, 0, len(step.Ingress.Rules))
	for _, rule := range step.Ingress.Rules {
		rules = append(rules, activity.IngressRouteRule{Path: rule.Path, ServiceName: plan.IDOf(templateplan.StepService, rule.Deploy), Port: int32(rule.Port)})
	}
	definition := activity.IngressRouteDefinition{Namespace: namespaceName, Name: step.ID, Host: plan.Hosts[0], Rules: rules}
	return definition, target.ingresses.ApplyIngressRoute(requestContext, definition)
}
