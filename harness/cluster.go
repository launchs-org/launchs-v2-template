package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	"launchs/worker/src/activity"
)

// connectCluster は、kubeconfig から、本物のクラスターへのクライアントを作る。
//
// 引数:
//   - kubeconfigPath: kubeconfig のパス(空なら、既定の場所と KUBECONFIG)。
//   - kubeContext: 使う context の名前(空なら、現在のもの)。
//
// 戻り値:
//   - 通常のクライアントと、動的なクライアント(IngressRoute 用)。
//   - 読み込めないときのエラー。
func connectCluster(kubeconfigPath string, kubeContext string) (kubernetes.Interface, dynamic.Interface, error) {
	// 既定の読み込みの規則に、指定を重ねる。
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}
	restConfig, configError := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: kubeContext}).ClientConfig()
	if configError != nil {
		return nil, nil, fmt.Errorf("kubeconfig を読めません: %w", configError)
	}
	// 2 種類のクライアントを作る。
	clientset, clientsetError := kubernetes.NewForConfig(restConfig)
	if clientsetError != nil {
		return nil, nil, clientsetError
	}
	dynamicClient, dynamicError := dynamic.NewForConfig(restConfig)
	return clientset, dynamicClient, dynamicError
}

// newFakeCluster は、クラスターを使わない、偽のクライアントを作る(render 用)。
//
// 戻り値:
//   - 偽の clientset と、偽の動的クライアント。
func newFakeCluster(traefikAPIVersion string) (*fake.Clientset, *dynamicfake.FakeDynamicClient) {
	// IngressRoute の一覧の型を、教えて作る。
	resource := schema.GroupVersionResource{Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutes"}
	if traefikAPIVersion == "traefik.containo.us/v1alpha1" {
		resource.Group = "traefik.containo.us"
	}
	return fake.NewSimpleClientset(), dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{resource: "IngressRouteList"})
}

// dumpManifests は、namespace に作られた資源を、YAML にして書き出す(render 用)。
//
// 引数:
//   - requestContext: 処理の context。
//   - clientset: 偽の clientset。
//   - namespaceName: 対象の namespace。
//   - dynamicClient: 偽の動的クライアント。
//   - traefikAPIVersion: IngressRoute の apiVersion。
//   - output: 書き出し先。
//
// 戻り値:
//   - 書き出せないときのエラー。
func dumpManifests(requestContext context.Context, clientset kubernetes.Interface, dynamicClient dynamic.Interface, traefikAPIVersion string, namespaceName string, output io.Writer) error {
	// 書き出す対象を、種類ごとに集める。
	objects := []any{}
	volumes, _ := clientset.CoreV1().PersistentVolumeClaims(namespaceName).List(requestContext, metav1.ListOptions{})
	for index := range volumes.Items {
		volumes.Items[index].TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"}
		objects = append(objects, volumes.Items[index])
	}
	configMaps, _ := clientset.CoreV1().ConfigMaps(namespaceName).List(requestContext, metav1.ListOptions{})
	for index := range configMaps.Items {
		configMaps.Items[index].TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}
		objects = append(objects, configMaps.Items[index])
	}
	services, _ := clientset.CoreV1().Services(namespaceName).List(requestContext, metav1.ListOptions{})
	for index := range services.Items {
		services.Items[index].TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}
		objects = append(objects, services.Items[index])
	}
	deployments, _ := clientset.AppsV1().Deployments(namespaceName).List(requestContext, metav1.ListOptions{})
	for index := range deployments.Items {
		deployments.Items[index].TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}
		deployments.Items[index].Status = appsv1.DeploymentStatus{}
		objects = append(objects, deployments.Items[index])
	}
	// IngressRoute は、動的クライアントから読む。
	group := "traefik.io"
	if traefikAPIVersion == "traefik.containo.us/v1alpha1" {
		group = "traefik.containo.us"
	}
	routes, routesError := dynamicClient.Resource(schema.GroupVersionResource{Group: group, Version: "v1alpha1", Resource: "ingressroutes"}).Namespace(namespaceName).List(requestContext, metav1.ListOptions{})
	if routesError == nil {
		for index := range routes.Items {
			objects = append(objects, routes.Items[index].Object)
		}
	}
	// 1 つずつ、YAML にして、「---」で区切る。
	for _, object := range objects {
		content, marshalError := yaml.Marshal(object)
		if marshalError != nil {
			return marshalError
		}
		fmt.Fprintf(output, "---\n%s", content)
	}
	return nil
}

// waitDeploymentsReady は、全ての Deployment が Ready になるのを待ち、その後、落ち着くまで見守る。
//
// 処理の流れ:
//  1. 全ての Deployment が、本番と同じ DeploymentReady の判定で、Ready になるまで、2 秒おきに確かめる。
//  2. Ready 後、settle の間、見守り、Pod の再起動(クラッシュの繰り返し)が起きたら失敗にする。
//
// 引数:
//   - requestContext: 処理の context。
//   - target: 本番と同じ operator を持つ runner。
//   - clientset: 本物の clientset。
//   - namespaceName: 対象の namespace。
//   - timeout: Ready を待つ上限。
//   - settle: Ready 後に見守る時間。
//
// 戻り値:
//   - 時間内に Ready にならない、または再起動したときのエラー(Pod の状態を含む)。
func waitDeploymentsReady(requestContext context.Context, target *runner, clientset kubernetes.Interface, namespaceName string, timeout time.Duration, settle time.Duration) error {
	// Deployment の名前を集める。
	deployments, listError := clientset.AppsV1().Deployments(namespaceName).List(requestContext, metav1.ListOptions{})
	if listError != nil {
		return listError
	}
	deadline := time.Now().Add(timeout)
	for _, deployment := range deployments.Items {
		// この Deployment が Ready になるまで、待つ。
		for {
			ready, readyError := target.deployments.DeploymentReady(requestContext, namespaceName, deployment.Name)
			if readyError != nil {
				return fmt.Errorf("%s: %w\n%s", deployment.Name, readyError, describePods(requestContext, clientset, namespaceName, deployment.Name))
			}
			if ready {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%s が、%s 以内に Ready になりません\n%s", deployment.Name, timeout, describePods(requestContext, clientset, namespaceName, deployment.Name))
			}
			time.Sleep(2 * time.Second)
		}
	}
	// 落ち着くまで、見守る。
	time.Sleep(settle)
	for _, deployment := range deployments.Items {
		pods, podsError := clientset.CoreV1().Pods(namespaceName).List(requestContext, metav1.ListOptions{LabelSelector: "launchs-org/deploy-id=" + deployment.Name})
		if podsError != nil {
			return podsError
		}
		for _, pod := range pods.Items {
			for _, status := range pod.Status.ContainerStatuses {
				if status.RestartCount > 0 {
					return fmt.Errorf("%s が、再起動を繰り返しています(%d 回)\n%s", deployment.Name, status.RestartCount, describePods(requestContext, clientset, namespaceName, deployment.Name))
				}
			}
		}
	}
	return nil
}

// describePods は、Deployment の Pod の状態を、失敗の説明のために、文字にする。
//
// 引数:
//   - requestContext: 処理の context。
//   - clientset: 本物の clientset。
//   - namespaceName: 対象の namespace。
//   - deployName: Deployment の名前。
//
// 戻り値:
//   - Pod ごとの、段階、コンテナの状態、直近のイベントの文字列。
func describePods(requestContext context.Context, clientset kubernetes.Interface, namespaceName string, deployName string) string {
	// Pod を探す。
	pods, listError := clientset.CoreV1().Pods(namespaceName).List(requestContext, metav1.ListOptions{LabelSelector: "launchs-org/deploy-id=" + deployName})
	if listError != nil {
		return "Pod を読めません: " + listError.Error()
	}
	text := ""
	for _, pod := range pods.Items {
		text += fmt.Sprintf("  Pod %s: %s\n", pod.Name, pod.Status.Phase)
		for _, status := range pod.Status.ContainerStatuses {
			text += fmt.Sprintf("    コンテナ: ready=%v 再起動=%d 待機=%s 終了=%s\n", status.Ready, status.RestartCount, waitingReason(status), terminatedReason(status))
		}
		// イベントを、新しい順に数件。
		events, _ := clientset.CoreV1().Events(namespaceName).List(requestContext, metav1.ListOptions{FieldSelector: "involvedObject.name=" + pod.Name})
		sort.Slice(events.Items, func(first int, second int) bool {
			return events.Items[first].LastTimestamp.After(events.Items[second].LastTimestamp.Time)
		})
		for index, event := range events.Items {
			if index >= 4 {
				break
			}
			text += fmt.Sprintf("    イベント: %s %s\n", event.Reason, event.Message)
		}
	}
	return text
}

// waitingReason は、コンテナが待機している理由を返す。
func waitingReason(status corev1.ContainerStatus) string {
	// 待機中でなければ、空。
	if status.State.Waiting == nil {
		return "-"
	}
	return status.State.Waiting.Reason + " " + status.State.Waiting.Message
}

// terminatedReason は、コンテナが終了した理由を返す。
func terminatedReason(status corev1.ContainerStatus) string {
	// 終了していなければ、空。
	if status.State.Terminated == nil {
		return "-"
	}
	return fmt.Sprintf("%s(終了コード %d)", status.State.Terminated.Reason, status.State.Terminated.ExitCode)
}

// checkHTTP は、ingress のルールの向き先に、API サーバーの proxy 経由で HTTP の要求を送り、成功するまで待つ。
//
// 引数:
//   - requestContext: 処理の context。
//   - clientset: 本物の clientset。
//   - namespaceName: 対象の namespace。
//   - definition: 作った ingress の定義。
//   - settings: 成功とみなすステータスと、待ち時間。
//
// 戻り値:
//   - 時間内に成功しないときのエラー(最後の結果を含む)。
func checkHTTP(requestContext context.Context, clientset kubernetes.Interface, namespaceName string, definition activity.IngressRouteDefinition, settings smokeSettings) error {
	deadline := time.Now().Add(time.Duration(settings.TimeoutSeconds) * time.Second)
	for _, rule := range definition.Rules {
		// 確かめるパス(設定があれば優先)。
		path := rule.Path
		if settings.Path != "" {
			path = settings.Path
		}
		last := ""
		for {
			// API サーバーの proxy を通して、Service に要求を送る(ステータスも取れる形で)。
			status := 0
			result := clientset.CoreV1().RESTClient().Get().Namespace(namespaceName).Resource("services").Name(fmt.Sprintf("http:%s:%d", rule.ServiceName, rule.Port)).SubResource("proxy").Suffix(path).Do(requestContext)
			result.StatusCode(&status)
			body, doError := result.Raw()
			// アプリが 401 や 403 などを返すと、proxy は、それを、API のエラーとして返す。その場合は、エラーの中のステータスを使う。
			var statusError apierrors.APIStatus
			if status == 0 && errors.As(doError, &statusError) {
				status = int(statusError.Status().Code)
			}
			response := body
			if status > 0 && statusAccepted(settings, status) {
				break
			}
			last = fmt.Sprintf("status=%d error=%v body=%.200s", status, doError, string(response))
			if time.Now().After(deadline) {
				return fmt.Errorf("GET %s(service %s:%d)が成功しません: %s", path, rule.ServiceName, rule.Port, last)
			}
			time.Sleep(3 * time.Second)
		}
	}
	return nil
}
